package collector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"identity-monitor-agent/internal/analyzer"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/extractor"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/imagehash"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/observability"
	"identity-monitor-agent/internal/sentiment"
	"identity-monitor-agent/internal/source"
	"identity-monitor-agent/internal/storage"
	"log/slog"
	"sync"
	"time"
)

type Supervisor struct {
	Config          config.Config
	Store           *storage.SQLite
	Analyzer        analyzer.Analyzer
	Sources         map[string]source.SourceAdapter
	Metrics         *observability.Metrics
	Images          *imagehash.Service
	mu              sync.Mutex
	active          map[string]string
	slots           chan struct{}
	wg              sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc
	batch           BatchProgress
	analysisBusy    int
	closing         bool
	sentiment       sentiment.Classifier
	sentimentWake   chan struct{}
	contextWake     chan struct{}
	analysisStarted bool
}

func New(ctx context.Context, c config.Config, db *storage.SQLite, a analyzer.Analyzer, sources map[string]source.SourceAdapter, m *observability.Metrics, classifiers ...sentiment.Classifier) *Supervisor {
	ctx, cancel := context.WithCancel(ctx)
	s := &Supervisor{Config: c, Store: db, Analyzer: a, Sources: sources, Metrics: m, Images: &imagehash.Service{Store: db, Threshold: c.ImageDistance}, active: map[string]string{}, slots: make(chan struct{}, c.Scheduler.MaxConcurrentJobs), ctx: ctx, cancel: cancel, sentimentWake: make(chan struct{}, 1), contextWake: make(chan struct{}, 1)}
	if len(classifiers) > 0 {
		s.sentiment = classifiers[0]
	}
	return s
}
func (s *Supervisor) Stop() { s.mu.Lock(); s.closing = true; s.cancel(); s.mu.Unlock(); s.wg.Wait() }
func (s *Supervisor) Active() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := map[string]string{}
	for k, v := range s.active {
		m[k] = v
	}
	return m
}
func (s *Supervisor) window(now time.Time) (time.Time, error) {
	loc, _ := time.LoadLocation(s.Config.App.Timezone)
	t := now.In(loc)
	start, _ := time.ParseInLocation("2006-01-02 15:04", t.Format("2006-01-02")+" "+s.Config.Scheduler.StartTime, loc)
	stop, _ := time.ParseInLocation("2006-01-02 15:04", t.Format("2006-01-02")+" "+s.Config.Scheduler.StopTime, loc)
	if t.Before(start) || !t.Before(stop) {
		return stop, fmt.Errorf("collection allowed only %s–%s %s", s.Config.Scheduler.StartTime, s.Config.Scheduler.StopTime, s.Config.App.Timezone)
	}
	return stop, nil
}
func (s *Supervisor) Submit(ctx context.Context, subjectID, sourceName, query string) (model.Run, error) {
	return s.SubmitFiltered(ctx, subjectID, sourceName, query, "", "")
}

func (s *Supervisor) SubmitFiltered(ctx context.Context, subjectID, sourceName, query, dateFrom, dateTo string) (model.Run, error) {
	return s.submitExecution(ctx, subjectID, sourceName, query, dateFrom, dateTo, "")
}
func (s *Supervisor) submitExecution(ctx context.Context, subjectID, sourceName, query, dateFrom, dateTo, executionID string) (model.Run, error) {
	if _, ok := s.Sources[sourceName]; !ok {
		return model.Run{}, fmt.Errorf("unknown or disabled source")
	}
	subjects, e := s.Store.Subjects(ctx)
	if e != nil {
		return model.Run{}, e
	}
	var subject model.Subject
	for _, v := range subjects {
		if v.ID == subjectID {
			subject = v
		}
	}
	if subject.ID == "" {
		return model.Run{}, fmt.Errorf("subject not found")
	}
	if query == "" {
		query = Keywords(subject)[0]
	}
	if len(query) > 1000 {
		return model.Run{}, fmt.Errorf("query too long")
	}
	if e = validateDateRange(dateFrom, dateTo); e != nil {
		return model.Run{}, e
	}
	if !s.Config.Demo && !s.Config.AllowOutsideHours {
		if _, e = s.window(time.Now()); e != nil {
			return model.Run{}, e
		}
	}
	if e = s.startAnalysis(); e != nil {
		return model.Run{}, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return model.Run{}, fmt.Errorf("collector stopping")
	}
	if _, ok := s.active[sourceName]; ok {
		return model.Run{}, fmt.Errorf("source already running; retry after completion")
	}
	select {
	case s.slots <- struct{}{}:
	default:
		return model.Run{}, fmt.Errorf("collector capacity reached")
	}
	b := make([]byte, 16)
	if _, e = rand.Read(b); e != nil {
		<-s.slots
		return model.Run{}, e
	}
	r := model.Run{ExecutionID: executionID, ID: hex.EncodeToString(b), SubjectID: subjectID, Source: sourceName, Query: query, DateFrom: dateFrom, DateTo: dateTo, Status: "PENDING", StartedAt: time.Now().UTC()}
	if r.ExecutionID == "" {
		r.ExecutionID = r.ID
	}
	if e = s.Store.CreateRun(ctx, r); e != nil {
		<-s.slots
		return model.Run{}, e
	}
	s.active[sourceName] = r.ID
	s.wg.Add(1)
	go s.execute(r, subject)
	return r, nil
}
func (s *Supervisor) execute(r model.Run, subject model.Subject) {
	defer s.wg.Done()
	defer func() { s.mu.Lock(); delete(s.active, r.Source); <-s.slots; s.mu.Unlock() }()
	start := time.Now()
	defer func() { s.Metrics.RunDuration.Observe(time.Since(start).Seconds()) }()
	s.Metrics.Inc("collector_runs_total")
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(s.Config.Scheduler.JobTimeoutSeconds)*time.Second)
	defer cancel()
	if !s.Config.Demo && !s.Config.AllowOutsideHours {
		stop, e := s.window(time.Now())
		if e != nil {
			r.Status = "CANCELLED"
			r.ErrorMessage = "outside collection window"
			_ = s.Store.UpdateRun(context.Background(), r)
			return
		}
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithDeadline(ctx, stop)
		defer deadlineCancel()
	}
	r.Status = "RUNNING"
	if e := s.Store.UpdateRun(ctx, r); e != nil {
		slog.Error("run start failed", "run_id", r.ID)
		return
	}
	log := slog.With("run_id", r.ID, "task_id", r.ID, "subject_id", subject.ID, "source", r.Source, "query_hash", extractor.Hash(r.Query))
	log.Info("collection started")
	maxResults := 10
	for _, src := range s.Config.Sources {
		if src.Name == r.Source {
			maxResults = src.MaxResults
		}
	}
	mentions, e := s.Sources[r.Source].Search(ctx, model.SearchTask{ID: r.ID, SubjectID: subject.ID, Source: r.Source, Query: r.Query, MaxResults: maxResults, DateFrom: r.DateFrom, DateTo: r.DateTo})
	mentions = filterMentionDates(mentions, r.DateFrom, r.DateTo, s.Store.Location)
	r.TotalDiscovered = len(mentions)
	s.Metrics.Counters["collector_mentions_discovered_total"].Add(float64(len(mentions)))
	if len(mentions) > 0 {
		for _, raw := range mentions {
			if ctxErr := ctx.Err(); ctxErr != nil {
				e = ctxErr
				break
			}
			normalized, ne := extractor.Normalize(raw)
			if ne != nil {
				r.TotalFailed++
				continue
			}
			m := model.Mention{SubjectID: subject.ID, RawMention: normalized, ContentHash: extractor.Hash(normalized.ContentText), Analysis: model.AnalysisResult{Sentiment: "unknown", RiskCategory: "unknown"}, AnalysisStatus: "PENDING"}
			_, duplicate, se := s.Store.SaveMentionForRun(ctx, m, s.Config.RiskThreshold, r.ID)
			if se != nil {
				r.TotalFailed++
				s.Metrics.Inc("collector_errors_total")
			} else if duplicate {
				r.TotalDuplicate++
				s.Metrics.Inc("collector_duplicates_total")
			} else {
				r.TotalStored++
				s.Metrics.Inc("collector_mentions_stored_total")
				s.notifyAnalysis()
			}
			if ue := s.Store.UpdateRun(ctx, r); ue != nil {
				e = ue
				break
			}
		}
	}
	r.Status = "COMPLETED"
	if e != nil || r.TotalFailed > 0 {
		r.Status = "FAILED"
		r.ErrorMessage = "collection or persistence failed; inspect local source/session"
		s.Metrics.Inc("collector_errors_total")
		if errors.Is(e, guardrail.ErrHuman) {
			r.Status = "HUMAN_ACTION_REQUIRED"
			r.ErrorMessage = "CAPTCHA, login, verification or disallowed action requires operator"
			s.Metrics.Inc("collector_human_action_required_total")
		} else if errors.Is(e, context.Canceled) {
			r.Status = "CANCELLED"
			r.ErrorMessage = "graceful shutdown"
		} else if errors.Is(e, context.DeadlineExceeded) {
			r.Status = "PAUSED"
			r.ErrorMessage = "work window or task timeout reached"
		}
	}
	finished := time.Now().UTC()
	r.FinishedAt = &finished
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	if ue := s.Store.UpdateRun(saveCtx, r); ue != nil {
		log.Error("run finalization failed")
	}
	_, n, _ := s.Store.Mentions(saveCtx, model.MentionFilter{Relevance: "active", Review: true, Limit: 1})
	s.Metrics.Review.Set(float64(n))
	log.Info("collection finished", "status", r.Status, "stored", r.TotalStored, "duplicate", r.TotalDuplicate)
}

func (s *Supervisor) WaitAnalysis(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		pending, e := s.Store.PendingAnalysisCount(ctx)
		if e != nil {
			return e
		}
		s.mu.Lock()
		busy := s.analysisBusy
		s.mu.Unlock()
		if pending == 0 && busy == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (s *Supervisor) Wait(ctx context.Context, id string) (model.Run, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		runs, e := s.Store.Runs(ctx)
		if e != nil {
			return model.Run{}, e
		}
		for _, r := range runs {
			if r.ID == id && r.Status != "PENDING" && r.Status != "RUNNING" {
				s.mu.Lock()
				active := s.active[r.Source] == id
				s.mu.Unlock()
				if !active {
					return r, nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return model.Run{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
func (s *Supervisor) Retry(ctx context.Context, id string) (model.Run, error) {
	runs, e := s.Store.Runs(ctx)
	if e != nil {
		return model.Run{}, e
	}
	for _, r := range runs {
		if r.ID == id {
			if !model.RunTransition(r.Status, "PENDING") {
				return model.Run{}, fmt.Errorf("run is not safely retryable")
			}
			return s.SubmitFiltered(ctx, r.SubjectID, r.Source, r.Query, r.DateFrom, r.DateTo)
		}
	}
	return model.Run{}, fmt.Errorf("run not found")
}

func validateDateRange(from, to string) error {
	var start, end time.Time
	var err error
	if from != "" {
		start, err = time.Parse("2006-01-02", from)
		if err != nil {
			return fmt.Errorf("date_from must be YYYY-MM-DD")
		}
	}
	if to != "" {
		end, err = time.Parse("2006-01-02", to)
		if err != nil {
			return fmt.Errorf("date_to must be YYYY-MM-DD")
		}
	}
	if !start.IsZero() && !end.IsZero() && start.After(end) {
		return fmt.Errorf("date_from must not be after date_to")
	}
	return nil
}

func filterMentionDates(in []model.RawMention, from, to string, location *time.Location) []model.RawMention {
	if from == "" && to == "" {
		return in
	}
	start, _ := time.ParseInLocation("2006-01-02", from, location)
	end, _ := time.ParseInLocation("2006-01-02", to, location)
	if to != "" {
		end = end.Add(24*time.Hour - time.Nanosecond)
	}
	out := make([]model.RawMention, 0, len(in))
	for _, mention := range in {
		// Sources that do not publish an exact timestamp remain visible instead of
		// being silently discarded. Dated results are filtered strictly.
		if mention.PublishedAt == nil || (from == "" || !mention.PublishedAt.In(location).Before(start)) && (to == "" || !mention.PublishedAt.In(location).After(end)) {
			out = append(out, mention)
		}
	}
	return out
}
func (s *Supervisor) Recover(ctx context.Context) error {
	runs, e := s.Store.Runs(ctx)
	if e != nil {
		return e
	}
	for _, r := range runs {
		switch r.Status {
		case "RUNNING":
			r.Status = "PAUSED"
			r.ErrorMessage = "interrupted; retry safely with deduplication"
			if e = s.Store.UpdateRun(ctx, r); e != nil {
				return e
			}
		case "PENDING":
			r.Status = "CANCELLED"
			r.ErrorMessage = "interrupted before start; scheduler will reschedule"
			if e = s.Store.UpdateRun(ctx, r); e != nil {
				return e
			}
		}
	}
	return s.startAnalysis()
}
