package app

import (
	"context"
	"fmt"
	"github.com/gofrs/flock"
	"identity-monitor-agent/internal/agent"
	"identity-monitor-agent/internal/analyzer"
	"identity-monitor-agent/internal/browser"
	"identity-monitor-agent/internal/collector"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/dashboard"
	"identity-monitor-agent/internal/demo"
	"identity-monitor-agent/internal/observability"
	"identity-monitor-agent/internal/ollama"
	"identity-monitor-agent/internal/report"
	"identity-monitor-agent/internal/securefs"
	"identity-monitor-agent/internal/sentiment"
	"identity-monitor-agent/internal/source"
	"identity-monitor-agent/internal/storage"
	"os"
	"path/filepath"
	"time"
)

type Runtime struct {
	Config     config.Config
	Store      *storage.SQLite
	Supervisor *collector.Supervisor
	Dashboard  *dashboard.Server
	Reports    *report.Service
	Browser    *browser.Manager
	sentiment  sentiment.Classifier
	closeMock  func()
	lock       *flock.Flock
}

func Open(ctx context.Context, c config.Config, subjectsPath string, needAI bool) (*Runtime, error) {
	for _, dir := range []string{c.App.DataDirectory, c.App.EvidenceDirectory, c.App.ReportDirectory} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			return nil, e
		}
		if e := securefs.Restrict(dir, true); e != nil {
			return nil, e
		}
	}
	db, e := storage.Open(filepath.Join(c.App.DataDirectory, "monitor.db"))
	if e != nil {
		return nil, e
	}
	db.Location, _ = time.LoadLocation(c.App.Timezone)
	r := &Runtime{Config: c, Store: db, closeMock: func() {}}
	success := false
	defer func() {
		if !success {
			r.Close()
		}
	}()
	if needAI {
		r.lock = flock.New(filepath.Join(c.App.DataDirectory, "collector.lock"))
		locked, err := r.lock.TryLock()
		if err != nil {
			return nil, err
		}
		if !locked {
			return nil, fmt.Errorf("another collector is active for this data directory")
		}
	}
	subjects, e := config.LoadSubjects(subjectsPath)
	if e != nil {
		return nil, e
	}
	for _, s := range subjects {
		if e = db.UpsertSubject(ctx, s); e != nil {
			return nil, e
		}
	}
	metrics := observability.New()
	ollamaConfig := c.Ollama
	if c.Demo {
		mock := demo.Server()
		r.closeMock = mock.Close
		ollamaConfig.BaseURL = mock.URL
		ollamaConfig.TextModel = "fixture-mock"
		ollamaConfig.VisionModel = ""
	}
	client := ollama.New(ollamaConfig, metrics)
	if needAI {
		healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = client.Health(healthCtx)
		cancel()
		if e != nil {
			return nil, e
		}
	}
	r.Browser = browser.New(c.Browser, c.App.EvidenceDirectory, metrics)
	adapters := map[string]source.SourceAdapter{}
	fallback := &agent.Loop{Client: client, Store: db, Metrics: metrics, Timeout: time.Duration(c.Guardrails.AgentTimeoutSeconds) * time.Second}
	for _, s := range c.Sources {
		if e = db.SourceConfig(ctx, s.Name, s); e != nil {
			return nil, e
		}
		if !s.Enabled {
			continue
		}
		if c.Demo {
			adapters[s.Name] = demo.Adapter{Source: s.Name}
		} else {
			adapters[s.Name] = &source.Adapter{Config: s, Browser: r.Browser, Fallback: fallback, Guardrails: c.Guardrails}
		}
	}
	var analysisService analyzer.Analyzer = &analyzer.Local{Client: client, Threshold: c.RiskThreshold}
	if needAI && !c.Demo && c.Sentiment.Provider == "indobert" {
		r.sentiment, e = sentiment.New(c.Sentiment)
		if e != nil {
			return nil, fmt.Errorf("initialize IndoBERT sentiment: %w", e)
		}
		analysisService = &analyzer.Hybrid{Base: analysisService, Sentiment: r.sentiment}
	}
	r.Supervisor = collector.New(ctx, c, db, analysisService, adapters, metrics)
	for _, subject := range subjects {
		if e = r.Supervisor.Images.References(ctx, subject); e != nil {
			return nil, e
		}
	}
	if needAI {
		if e = r.Supervisor.Recover(ctx); e != nil {
			return nil, e
		}
	}
	r.Reports = &report.Service{Store: db, Directory: c.App.ReportDirectory, Threshold: c.RiskThreshold, Demo: c.Demo}
	r.Dashboard = dashboard.New(c, db, r.Supervisor, client, r.Reports, metrics)
	success = true
	return r, nil
}
func (r *Runtime) Close() {
	if r.Supervisor != nil {
		r.Supervisor.Stop()
	}
	if r.sentiment != nil {
		_ = r.sentiment.Close()
	}
	if r.Store != nil {
		_ = r.Store.Close()
	}
	if r.closeMock != nil {
		r.closeMock()
	}
	if r.lock != nil {
		_ = r.lock.Unlock()
	}
}
func (r *Runtime) Demo(ctx context.Context) error {
	subjects, e := r.Store.Subjects(ctx)
	if e != nil {
		return e
	}
	if len(subjects) == 0 {
		return fmt.Errorf("demo requires one subject")
	}
	for _, name := range []string{"generic_web_search", "youtube_public_search", "generic_web_search"} {
		run, e := r.Supervisor.Submit(ctx, subjects[0].ID, name, "")
		if e != nil {
			return e
		}
		finished, e := r.Supervisor.Wait(ctx, run.ID)
		if e != nil {
			return e
		}
		if finished.Status != "COMPLETED" {
			return fmt.Errorf("demo run %s: %s", finished.Status, finished.ErrorMessage)
		}
	}
	_, e = r.Reports.Generate(ctx, time.Now().In(r.Store.Location).Format("2006-01-02"))
	return e
}
