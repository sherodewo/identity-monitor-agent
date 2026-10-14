package collector

import (
	"context"
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/observability"
	"identity-monitor-agent/internal/sentiment"
	"identity-monitor-agent/internal/storage"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fastSentiment struct {
	calls atomic.Int32
	fail  bool
}

func (f *fastSentiment) Close() error { return nil }
func (f *fastSentiment) Classify(context.Context, string) (sentiment.Result, error) {
	f.calls.Add(1)
	if f.fail {
		return sentiment.Result{}, errors.New("sentiment unavailable")
	}
	return sentiment.Result{Label: "positive", Score: 0.98, Confidence: 0.99, Model: "test-indobert"}, nil
}

type gatedContext struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	fail    bool
	calls   atomic.Int32
}

func (g *gatedContext) Analyze(ctx context.Context, _ model.Subject, _ model.RawMention) (model.AnalysisResult, error) {
	g.calls.Add(1)
	g.once.Do(func() { close(g.started) })
	select {
	case <-ctx.Done():
		return model.AnalysisResult{}, ctx.Err()
	case <-g.release:
	}
	a := model.AnalysisResult{Sentiment: "negative", SentimentScore: -1, Relevant: true, RiskCategory: "normal_mention", Summary: "context ready", IdentityConfidence: 0.99}
	if g.fail {
		return a, errors.New("ollama unavailable")
	}
	return a, nil
}
func seedPipeline(t *testing.T, n int) (*storage.SQLite, config.Config) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "pipeline.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "one", FullName: "One"}))
	for i := 0; i < n; i++ {
		_, _, err = db.SaveMention(ctx, model.Mention{SubjectID: "one", ContentHash: fmt.Sprint(i), AnalysisStatus: "PENDING", RawMention: model.RawMention{Source: "test", SourceURL: fmt.Sprintf("https://example.org/%d", i), ContentText: fmt.Sprintf("posting %d", i), CollectedAt: time.Now()}, Analysis: model.AnalysisResult{Sentiment: "unknown", RiskCategory: "unknown"}}, 70)
		require.NoError(t, err)
	}
	c := config.Defaults()
	c.Demo = true
	return db, c
}
func TestSentimentContinuesWhileContextBlockedAndSurvivesRestart(t *testing.T) {
	db, c := seedPipeline(t, 3)
	ctx := context.Background()
	sent := &fastSentiment{}
	blocked := &gatedContext{started: make(chan struct{}), release: make(chan struct{})}
	s := New(ctx, c, db, blocked, nil, observability.New(), sent)
	t.Cleanup(s.Stop)
	require.NoError(t, s.Recover(ctx))
	select {
	case <-blocked.started:
	case <-time.After(10 * time.Second):
		t.Fatal("context did not start")
	}
	require.Eventually(t, func() bool {
		p, e := db.Progress(ctx)
		return e == nil && p.Sentiment.Completed == 3 && p.Context.Running == 1 && p.Context.Pending == 2
	}, 10*time.Second, 10*time.Millisecond)
	mentions, n, err := db.Mentions(ctx, model.MentionFilter{Sentiment: "positive"})
	require.NoError(t, err)
	require.Equal(t, 3, n)
	for _, m := range mentions {
		require.Equal(t, "COMPLETED", m.Analysis.SentimentStatus)
		require.NotEqual(t, "COMPLETED", m.Analysis.ContextStatus)
	}
	_, err = s.ClearData(ctx, "")
	require.Error(t, err)
	s.Stop()
	// Resume only the context stage; the persisted sentiment is never recomputed.
	nextSent := &fastSentiment{}
	ready := &gatedContext{started: make(chan struct{}), release: make(chan struct{})}
	close(ready.release)
	resumed := New(ctx, c, db, ready, nil, observability.New(), nextSent)
	defer resumed.Stop()
	require.NoError(t, resumed.Recover(ctx))
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	require.NoError(t, resumed.WaitAnalysis(waitCtx))
	require.EqualValues(t, 0, nextSent.calls.Load())
	require.EqualValues(t, 3, ready.calls.Load())
	mentions, n, err = db.Mentions(ctx, model.MentionFilter{Sentiment: "positive"})
	require.NoError(t, err)
	require.Equal(t, 3, n)
	for _, m := range mentions {
		require.Equal(t, "COMPLETED", m.AnalysisStatus)
		require.Equal(t, "context ready", m.Analysis.Summary)
		require.Equal(t, "test-indobert", m.Analysis.SentimentModel)
		require.Equal(t, 0.99, m.Analysis.SentimentConfidence)
	}
}
func TestStageFailuresRemainIndependent(t *testing.T) {
	for _, sentimentFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("sentiment_fails_%v", sentimentFails), func(t *testing.T) {
			db, c := seedPipeline(t, 1)
			ctx := context.Background()
			sent := &fastSentiment{fail: sentimentFails}
			base := &gatedContext{started: make(chan struct{}), release: make(chan struct{}), fail: !sentimentFails}
			close(base.release)
			s := New(ctx, c, db, base, nil, observability.New(), sent)
			defer s.Stop()
			require.NoError(t, s.Recover(ctx))
			waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			require.NoError(t, s.WaitAnalysis(waitCtx))
			m, err := db.Mention(ctx, 1)
			require.NoError(t, err)
			require.Equal(t, "ANALYSIS_FAILED", m.AnalysisStatus)
			require.True(t, m.Analysis.RequiresReview)
			p, err := db.Progress(ctx)
			require.NoError(t, err)
			if sentimentFails {
				require.Equal(t, "FAILED", m.Analysis.SentimentStatus)
				require.Equal(t, "COMPLETED", m.Analysis.ContextStatus)
				require.Equal(t, "unknown", m.Analysis.Sentiment)
				require.Equal(t, 1, p.Sentiment.Failed)
				require.Equal(t, 1, p.Context.Completed)
			} else {
				require.Equal(t, "COMPLETED", m.Analysis.SentimentStatus)
				require.Equal(t, "FAILED", m.Analysis.ContextStatus)
				require.Equal(t, "positive", m.Analysis.Sentiment)
				require.Equal(t, 1, p.Sentiment.Completed)
				require.Equal(t, 1, p.Context.Failed)
			}
		})
	}
}
