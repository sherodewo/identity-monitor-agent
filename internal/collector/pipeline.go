package collector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"identity-monitor-agent/internal/analyzer"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/sentiment"
	"log/slog"
	"time"
)

func (s *Supervisor) startAnalysis() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return fmt.Errorf("collector stopping")
	}
	if s.analysisStarted {
		return nil
	}
	if err := s.Store.RecoverAnalysis(s.ctx); err != nil {
		return err
	}
	s.analysisStarted = true
	s.wg.Add(1)
	go s.stageWorker("context", s.contextWake)
	if s.sentiment != nil {
		s.wg.Add(1)
		go s.stageWorker("sentiment", s.sentimentWake)
	}
	return nil
}
func (s *Supervisor) notifyAnalysis() {
	select {
	case s.sentimentWake <- struct{}{}:
	default:
	}
	select {
	case s.contextWake <- struct{}{}:
	default:
	}
}
func (s *Supervisor) stageWorker(stage string, wake <-chan struct{}) {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for s.ctx.Err() == nil {
		// Include DB claims and image processing in the busy count for safe deletion.
		s.mu.Lock()
		s.analysisBusy++
		s.mu.Unlock()
		mention, err := s.Store.ClaimAnalysis(s.ctx, stage, s.sentiment != nil)
		if err == nil {
			err = s.processStage(stage, mention)
		}
		s.mu.Lock()
		s.analysisBusy--
		s.mu.Unlock()
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) && s.ctx.Err() == nil {
			slog.Error("analysis worker failed", "stage", stage, "error", err)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-wake:
		case <-ticker.C:
		}
	}
}
func (s *Supervisor) processStage(stage string, m model.Mention) (err error) {
	// If shutdown or persistence interrupts a stage, leave its durable job ready
	// for the next attempt. Never repeat a sentiment stage that already committed.
	committed := false
	defer func() {
		if !committed {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if e := s.Store.ReleaseAnalysis(ctx, m.ID, stage, s.sentiment != nil); e != nil {
				slog.Error("release analysis job failed", "mention_id", m.ID, "error", e)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(s.Config.Guardrails.AgentTimeoutSeconds)*time.Second)
	defer cancel()
	if stage == "sentiment" {
		result, inferErr := s.sentiment.Classify(ctx, m.ContentText)
		if s.ctx.Err() != nil {
			return s.ctx.Err()
		}
		if inferErr != nil {
			result = sentiment.Result{Label: "unknown", Model: "indobert:error"}
			slog.Warn("sentiment failed", "mention_id", m.ID, "error", inferErr)
		}
		saveCtx, saveCancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer saveCancel()
		if err = s.Store.CompleteSentiment(saveCtx, m.ID, result, inferErr != nil); err != nil {
			return err
		}
		committed = true
		s.notifyAnalysis()
		return nil
	}
	subjects, err := s.Store.Subjects(ctx)
	if err != nil {
		return err
	}
	var subject model.Subject
	for _, v := range subjects {
		if v.ID == m.SubjectID {
			subject = v
			break
		}
	}
	var result model.AnalysisResult
	var inferErr error
	if subject.ID == "" {
		result = analyzer.Failed("context")
		inferErr = fmt.Errorf("subject not found")
	} else {
		result, inferErr = s.Analyzer.Analyze(ctx, subject, m.RawMention)
	}
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	if m.RawMetadata["extraction_confidence"] == "low" {
		result.RequiresReview = true
	}
	status := "COMPLETED"
	if inferErr != nil {
		status = "ANALYSIS_FAILED"
		result.RequiresReview = true
		slog.Warn("context analysis failed", "mention_id", m.ID, "error", inferErr)
	}
	saveCtx, saveCancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer saveCancel()
	if err = s.Store.CompleteAnalysis(saveCtx, m.ID, result, status, s.Config.RiskThreshold); err != nil {
		return err
	}
	committed = true
	s.Metrics.Inc("analysis_results_total")
	if result.Relevant && (result.RiskScore >= s.Config.RiskThreshold || result.RiskCategory == "possible_impersonation" || result.RiskCategory == "possible_scam" || result.RiskCategory == "possible_doxxing" || result.RiskCategory == "possible_threat") {
		s.Metrics.Inc("cases_total")
	}
	if _, n, e := s.Store.Mentions(saveCtx, model.MentionFilter{Relevance: "active", Review: true, Limit: 1}); e == nil {
		s.Metrics.Review.Set(float64(n))
	}
	if inferErr == nil && !result.Relevant {
		return nil
	}
	var domains []string
	for _, src := range s.Config.Sources {
		if src.Name == m.Source {
			domains = src.AllowedDomains
			break
		}
	}
	if e := s.Images.Candidates(saveCtx, m.ID, subject, m.MediaURLs, domains); e != nil {
		slog.Warn("image comparison failed", "mention_id", m.ID, "error", e)
	}
	return nil
}
