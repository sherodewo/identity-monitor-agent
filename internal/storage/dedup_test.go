package storage

import (
	"context"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestDedupUnicodeAndLegacyBackfill(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dedup.db")
	db, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "one"}))
	m := model.Mention{SubjectID: "one", ContentHash: "old-hash", AnalysisStatus: "PENDING", RawMention: model.RawMention{Source: "threads", SourceURL: "https://www.threads.net/@a/post/123/?xmt=abc", ContentText: "Café sangat baik", CollectedAt: time.Now()}, Analysis: model.AnalysisResult{Sentiment: "unknown"}}
	_, dup, err := db.SaveMention(ctx, m, 70)
	require.NoError(t, err)
	require.False(t, dup)
	// Simulate pre-upgrade data, then reopen to populate the new index.
	_, err = db.DB.Exec("DELETE FROM mention_dedup")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	db, err = Open(path)
	require.NoError(t, err)
	defer db.Close()
	m.SourceURL = "https://www.threads.com/@a/post/456"
	m.ContentHash = "new-hash"
	m.ContentText = "  CAFE\u0301\u200b sangat\u00a0baik "
	_, dup, err = db.SaveMention(ctx, m, 70)
	require.NoError(t, err)
	require.True(t, dup)
	m.ContentText = "Konten berubah"
	m.SourceURL = "https://www.threads.com/@a/post/123?xmt=other"
	_, dup, err = db.SaveMention(ctx, m, 70)
	require.NoError(t, err)
	require.True(t, dup)
	m.SourceURL = "https://www.threads.com/@a/post/789"
	m.ContentText = "Café tidak baik"
	_, dup, err = db.SaveMention(ctx, m, 70)
	require.NoError(t, err)
	require.False(t, dup)
	p, err := db.Progress(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, p.Pending)
}
