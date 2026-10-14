package collector

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/observability"
	"identity-monitor-agent/internal/source"
	"identity-monitor-agent/internal/storage"
)

type instantSource struct{}

func (instantSource) Name() string { return "test" }
func (instantSource) CheckSession(context.Context) model.SessionStatus {
	return model.SessionReady
}
func (instantSource) Search(context.Context, model.SearchTask) ([]model.RawMention, error) {
	return []model.RawMention{{Source: "test", SourceURL: "https://example.test/post/1", ContentText: "contoh posting", CollectedAt: time.Now()}}, nil
}

type blockingAnalyzer struct {
	started chan struct{}
	release chan struct{}
}

func (a *blockingAnalyzer) Analyze(ctx context.Context, _ model.Subject, _ model.RawMention) (model.AnalysisResult, error) {
	close(a.started)
	select {
	case <-ctx.Done():
		return model.AnalysisResult{}, ctx.Err()
	case <-a.release:
		return model.AnalysisResult{Relevant: true, Sentiment: "positive", RiskCategory: "normal_mention", Summary: "selesai"}, nil
	}
}

func TestScrapingCompletesBeforeBackgroundAnalysis(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "monitor.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	subject := model.Subject{ID: "subject-1", FullName: "Subject One"}
	require.NoError(t, db.UpsertSubject(ctx, subject))

	c := config.Defaults()
	c.Demo = true
	c.Sources = []config.Source{{Name: "test", Enabled: true, AllowedDomains: []string{"example.test"}, MaxResults: 1}}
	a := &blockingAnalyzer{started: make(chan struct{}), release: make(chan struct{})}
	s := New(ctx, c, db, a, map[string]source.SourceAdapter{"test": instantSource{}}, observability.New())
	t.Cleanup(s.Stop)

	run, err := s.Submit(ctx, subject.ID, "test", "query")
	require.NoError(t, err)
	select {
	case <-a.started:
	case <-time.After(2 * time.Second):
		t.Fatal("background analyzer did not start")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	finished, err := s.Wait(waitCtx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "COMPLETED", finished.Status)
	require.Equal(t, 1, finished.TotalStored)

	mentions, _, err := db.Mentions(ctx, model.MentionFilter{})
	require.NoError(t, err)
	require.Equal(t, "ANALYZING", mentions[0].AnalysisStatus)
	close(a.release)
	require.NoError(t, s.WaitAnalysis(waitCtx))
	mention, err := db.Mention(ctx, mentions[0].ID)
	require.NoError(t, err)
	require.Equal(t, "COMPLETED", mention.AnalysisStatus)
	require.Equal(t, "positive", mention.Analysis.Sentiment)
}

func TestBatchContinuesWithoutClientWhileAnalysisBlocked(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "monitor.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "one", FullName: "One"}))
	c := config.Defaults()
	c.Demo = true
	a := &blockingAnalyzer{started: make(chan struct{}), release: make(chan struct{})}
	s := New(ctx, c, db, a, map[string]source.SourceAdapter{"test": instantSource{}}, observability.New())
	defer s.Stop()
	requestCtx, cancel := context.WithCancel(ctx)
	_, err = s.SubmitBatch(requestCtx, "one", "test", []string{"first", "second", "FIRST"}, "", "")
	require.NoError(t, err)
	cancel() // The browser request ends; no further polling or submissions.
	require.Eventually(t, func() bool { b := s.Batch(); return !b.Running && b.Completed == 2 }, 3*time.Second, 10*time.Millisecond)
	p, err := db.Progress(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, p.Total)
	require.Equal(t, 1, p.Duplicates)
	require.Equal(t, 0, p.Completed)
	close(a.release)
	waitCtx, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	require.NoError(t, s.WaitAnalysis(waitCtx))
}
