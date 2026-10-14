package storage

import (
	"context"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/sentiment"
	"path/filepath"
	"testing"
	"time"
)

func TestPipelineRecoveryAndManualCasePreserveSentiment(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "stages.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "s"}))
	id, _, err := db.SaveMention(ctx, model.Mention{SubjectID: "s", ContentHash: "h", AnalysisStatus: "PENDING", RawMention: model.RawMention{Source: "test", SourceURL: "https://example.org/post", ContentText: "Example", CollectedAt: time.Now()}, Analysis: model.AnalysisResult{Sentiment: "unknown"}}, 70)
	require.NoError(t, err)
	_, err = db.ClaimAnalysis(ctx, "sentiment", true)
	require.NoError(t, err)
	require.NoError(t, db.RecoverAnalysis(ctx)) // Interrupted before sentiment commit.
	m, err := db.ClaimAnalysis(ctx, "sentiment", true)
	require.NoError(t, err)
	require.Equal(t, id, m.ID)
	require.NoError(t, db.CompleteSentiment(ctx, id, sentiment.Result{Label: "positive", Confidence: 0.98, Score: 0.97, Model: "indobert"}, false))
	m, err = db.ClaimAnalysis(ctx, "context", true)
	require.NoError(t, err)
	require.Equal(t, "positive", m.Analysis.Sentiment)
	require.NoError(t, db.RecoverAnalysis(ctx)) // Interrupted after sentiment commit.
	_, err = db.ClaimAnalysis(ctx, "sentiment", true)
	require.Error(t, err)
	m, err = db.ClaimAnalysis(ctx, "context", true)
	require.NoError(t, err)
	require.Equal(t, "positive", m.Analysis.Sentiment)
	_, err = db.CreateCase(ctx, id, "reviewer")
	require.NoError(t, err)
	require.NoError(t, db.CompleteAnalysis(ctx, id, model.AnalysisResult{Sentiment: "negative", Relevant: true, RiskScore: 90, RiskCategory: "possible_scam"}, "COMPLETED", 70))
	m, err = db.Mention(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "positive", m.Analysis.Sentiment)
	require.Equal(t, 0.98, m.Analysis.SentimentConfidence)
	require.Equal(t, "COMPLETED", m.Analysis.ContextStatus)
	cases, err := db.Cases(ctx)
	require.NoError(t, err)
	require.Len(t, cases, 1)
}
