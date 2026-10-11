package storage

import (
	"context"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestRepositoryTransactions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")
	db, e := Open(path)
	require.NoError(t, e)
	defer db.Close()
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "one", FullName: "One", Aliases: []string{"A"}}))
	var fk int
	require.NoError(t, db.DB.QueryRow("PRAGMA foreign_keys").Scan(&fk))
	require.Equal(t, 1, fk)
	m := model.Mention{SubjectID: "one", ContentHash: "hash", AnalysisStatus: "COMPLETED", RawMention: model.RawMention{Source: "test", SourceURL: "https://example.org/1", CollectedAt: time.Now()}, Analysis: model.AnalysisResult{Relevant: true, RiskScore: 90, Sentiment: "negative", RiskCategory: "possible_scam", RequiresReview: true}}
	id, dup, e := db.SaveMention(ctx, m, 70)
	require.NoError(t, e)
	require.False(t, dup)
	_, dup, e = db.SaveMention(ctx, m, 70)
	require.NoError(t, e)
	require.True(t, dup)
	m.SourceURL = "https://example.org/2"
	_, dup, e = db.SaveMention(ctx, m, 70)
	require.NoError(t, e)
	require.True(t, dup)
	cases, e := db.Cases(ctx)
	require.NoError(t, e)
	require.Len(t, cases, 1)
	require.Equal(t, id, cases[0].MentionID)
	require.Error(t, db.TransitionCase(ctx, cases[0].ID, "APPROVED", "reviewer"))
	for _, status := range []string{"WAITING_APPROVAL", "APPROVED", "SUBMITTED_MANUALLY", "REMOVED", "CLOSED"} {
		require.NoError(t, db.TransitionCase(ctx, cases[0].ID, status, "reviewer"))
	}
	audit, e := db.CaseAudit(ctx, cases[0].ID)
	require.NoError(t, e)
	require.Len(t, audit, 6)
	require.NoError(t, db.Retain(ctx, 1))
	m.SubjectID = "nonexistent"
	m.ContentHash = "new"
	_, _, e = db.SaveMention(ctx, m, 70)
	require.Error(t, e)
	_, n, e := db.Mentions(ctx, model.MentionFilter{Review: true})
	require.NoError(t, e)
	require.Equal(t, 1, n)
	require.NoError(t, db.migrate())
}
