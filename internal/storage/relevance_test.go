package storage

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/sentiment"
	"testing"
	"time"
)

func TestRelevanceWaitsForContextAndPreservesExcludedData(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "s"}))
	for _, exec := range []string{"a", "b"} {
		require.NoError(t, db.CreateRun(ctx, model.Run{ID: exec, ExecutionID: exec, SubjectID: "s", Source: "test", Query: exec, Status: "COMPLETED"}))
	}
	m := model.Mention{SubjectID: "s", ContentHash: "h", AnalysisStatus: "PENDING", RawMention: model.RawMention{Source: "test", SourceURL: "https://example.org/post", ContentText: "Different person", CollectedAt: time.Now()}, Analysis: model.AnalysisResult{Sentiment: "unknown"}}
	id, _, err := db.SaveMentionForRun(ctx, m, 70, "a")
	require.NoError(t, err)
	_, err = db.ClaimAnalysis(ctx, "sentiment", true)
	require.NoError(t, err)
	require.NoError(t, db.CompleteSentiment(ctx, id, sentiment.Result{Label: "negative", Model: "indobert", Confidence: 0.99}, false))
	_, n, err := db.Mentions(ctx, model.MentionFilter{Execution: "a", Relevance: "active"})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, n, err = db.Mentions(ctx, model.MentionFilter{Relevance: "relevant"})
	require.NoError(t, err)
	require.Zero(t, n)
	_, err = db.ClaimAnalysis(ctx, "context", true)
	require.NoError(t, err)
	require.NoError(t, db.CompleteAnalysis(ctx, id, model.AnalysisResult{Relevant: false, Summary: "Different person with the same first name", RiskCategory: "unknown"}, "COMPLETED", 70))
	for _, visibility := range []string{"active", "relevant", "pending"} {
		_, n, err = db.Mentions(ctx, model.MentionFilter{Relevance: visibility})
		require.NoError(t, err)
		require.Zero(t, n)
	}
	rows, n, err := db.Mentions(ctx, model.MentionFilter{Execution: "a", Relevance: "excluded"})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "excluded", rows[0].Relevance)
	require.Equal(t, "negative", rows[0].Analysis.Sentiment)
	_, n, err = db.Mentions(ctx, model.MentionFilter{Execution: "b", Relevance: "excluded"})
	require.NoError(t, err)
	require.Zero(t, n)
	saved, err := db.Mention(ctx, id)
	require.NoError(t, err)
	require.Contains(t, saved.Analysis.Summary, "Different person")
	_, dup, err := db.SaveMentionForRun(ctx, m, 70, "b")
	require.NoError(t, err)
	require.True(t, dup)
	_, n, err = db.Mentions(ctx, model.MentionFilter{Execution: "b", Relevance: "excluded"})
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
func TestFailedContextIsNotExcluded(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "s"}))
	tests := []struct {
		status, contextStatus string
		relevant              bool
		want                  string
	}{
		{"ANALYSIS_FAILED", "FAILED", false, "pending"},
		{"ANALYSIS_FAILED", "COMPLETED", false, "excluded"},
		{"ANALYSIS_FAILED", "COMPLETED", true, "relevant"},
		{"COMPLETED", "", false, "excluded"},
		{"COMPLETED", "", true, "relevant"},
	}
	for i, test := range tests {
		id, _, err := db.SaveMention(ctx, model.Mention{SubjectID: "s", ContentHash: fmt.Sprint(i), AnalysisStatus: test.status, RawMention: model.RawMention{Source: "test", SourceURL: fmt.Sprintf("https://example.org/%d", i), ContentText: fmt.Sprint(i), CollectedAt: time.Now()}, Analysis: model.AnalysisResult{Relevant: test.relevant, ContextStatus: test.contextStatus, Sentiment: "positive"}}, 70)
		require.NoError(t, err)
		m, err := db.Mention(ctx, id)
		require.NoError(t, err)
		require.Equal(t, test.want, m.Relevance)
		rows, _, err := db.Mentions(ctx, model.MentionFilter{Relevance: test.want})
		require.NoError(t, err)
		found := false
		for _, r := range rows {
			if r.ID == id {
				found = true
			}
		}
		require.True(t, found)
	}
}

func TestReviewerCanOverrideRelevanceAndRemoveReviewQueueItem(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "s"}))
	id, _, err := db.SaveMention(ctx, model.Mention{SubjectID: "s", ContentHash: "manual", AnalysisStatus: "ANALYSIS_FAILED", RawMention: model.RawMention{Source: "test", SourceURL: "https://example.org/manual", CollectedAt: time.Now()}, Analysis: model.AnalysisResult{RequiresReview: true, ContextStatus: "FAILED", Sentiment: "unknown"}}, 70)
	require.NoError(t, err)

	require.NoError(t, db.ReviewRelevance(ctx, id, "relevant", "reviewer"))
	m, err := db.Mention(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "relevant", m.Relevance)
	require.Equal(t, "relevant", m.Analysis.ManualRelevance)
	_, count, err := db.Mentions(ctx, model.MentionFilter{Relevance: "relevant"})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	_, count, err = db.Mentions(ctx, model.MentionFilter{Relevance: "active", Review: true})
	require.NoError(t, err)
	require.Zero(t, count)

	require.NoError(t, db.ReviewRelevance(ctx, id, "excluded", "reviewer"))
	_, count, err = db.Mentions(ctx, model.MentionFilter{Relevance: "excluded"})
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
