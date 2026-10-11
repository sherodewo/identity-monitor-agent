package scheduler

import (
	"context"
	"fmt"
	"github.com/robfig/cron/v3"
	"identity-monitor-agent/internal/collector"
	"identity-monitor-agent/internal/evidence"
	"identity-monitor-agent/internal/report"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Scheduler struct {
	cron       *cron.Cron
	Supervisor *collector.Supervisor
	Reports    *report.Service
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
}

func New(ctx context.Context, s *collector.Supervisor, r *report.Service) (*Scheduler, error) {
	loc, e := time.LoadLocation(s.Config.App.Timezone)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(ctx)
	v := &Scheduler{cron: cron.New(cron.WithLocation(loc), cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger))), Supervisor: s, Reports: r, ctx: ctx, cancel: cancel}
	if _, e = v.cron.AddFunc(fmt.Sprintf("@every %dm", s.Config.Scheduler.IntervalMinutes), v.Collect); e != nil {
		cancel()
		return nil, e
	}
	startParts := strings.Split(s.Config.Scheduler.StartTime, ":")
	if _, e = v.cron.AddFunc(startParts[1]+" "+startParts[0]+" * * *", v.Collect); e != nil {
		cancel()
		return nil, e
	}
	parts := strings.Split(s.Config.Scheduler.DailyReportTime, ":")
	if _, e = v.cron.AddFunc(parts[1]+" "+parts[0]+" * * *", v.Daily); e != nil {
		cancel()
		return nil, e
	}
	if _, e = v.cron.AddFunc("@every 1m", func() {
		if e := s.Store.Event(ctx, "heartbeat", "collector alive"); e != nil {
			slog.Error("heartbeat failed")
		}
	}); e != nil {
		cancel()
		return nil, e
	}
	return v, nil
}
func (s *Scheduler) Start() { s.cron.Start() }
func (s *Scheduler) Stop()  { s.cancel(); <-s.cron.Stop().Done() }
func (s *Scheduler) Collect() {
	if !s.mu.TryLock() {
		return
	}
	defer s.mu.Unlock()
	subjects, e := s.Supervisor.Store.Subjects(s.ctx)
	if e != nil {
		return
	}
	runs, e := s.Supervisor.Store.Runs(s.ctx)
	if e != nil {
		return
	}
	latest := map[string]string{}
	for _, r := range runs {
		if _, exists := latest[r.Source]; !exists {
			latest[r.Source] = r.Status
		}
	}
	for _, subject := range subjects {
		for _, src := range s.Supervisor.Config.Sources {
			if !src.Enabled || latest[src.Name] == "HUMAN_ACTION_REQUIRED" {
				continue
			}
			for _, q := range collector.Keywords(subject) {
				run, e := s.Supervisor.Submit(s.ctx, subject.ID, src.Name, q)
				if e != nil {
					return
				}
				finished, e := s.Supervisor.Wait(s.ctx, run.ID)
				if e != nil {
					return
				}
				if finished.Status == "HUMAN_ACTION_REQUIRED" || finished.Status == "PAUSED" {
					latest[src.Name] = finished.Status
					break
				}
				select {
				case <-s.ctx.Done():
					return
				case <-time.After(time.Duration(s.Supervisor.Config.Browser.DelayMilliseconds) * time.Millisecond):
				}
			}
		}
	}
}
func (s *Scheduler) Daily() {
	date := time.Now().In(s.Supervisor.Store.Location).Format("2006-01-02")
	if _, e := s.Reports.Generate(s.ctx, date); e != nil {
		slog.Error("daily report failed")
	}
	if e := s.Supervisor.Store.Retain(s.ctx, s.Supervisor.Config.Retention.MentionDays); e != nil {
		slog.Error("mention retention failed")
	}
	protected := map[string]bool{}
	cases, e := s.Supervisor.Store.Cases(s.ctx)
	if e == nil {
		for _, c := range cases {
			m, e := s.Supervisor.Store.Mention(s.ctx, c.MentionID)
			if e == nil {
				protected[m.ScreenshotPath] = true
			}
		}
	}
	if e = evidence.Retain(s.Supervisor.Config.App.EvidenceDirectory, s.Supervisor.Config.Retention.ScreenshotDays, protected); e != nil {
		slog.Error("screenshot retention failed")
	}
	if e = s.Supervisor.Store.ArchiveAudit(s.ctx, filepath.Join(s.Supervisor.Config.App.DataDirectory, "audit-archives"), s.Supervisor.Config.Retention.AuditLogDays); e != nil {
		slog.Error("audit archive failed")
	}
}
