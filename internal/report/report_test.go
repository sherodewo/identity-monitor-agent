package report

import (
	"context"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/storage"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReportAndTimezone(t *testing.T) {
	db, e := storage.Open(":memory:")
	require.NoError(t, e)
	defer db.Close()
	ctx := context.Background()
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "s", FullName: "S"}))
	at := time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC)
	_, _, e = db.SaveMention(ctx, model.Mention{SubjectID: "s", ContentHash: "hash", AnalysisStatus: "COMPLETED", RawMention: model.RawMention{Source: "fixture", SourceURL: "https://example.org", ContentText: "=HYPERLINK(unsafe)", CollectedAt: at}, Analysis: model.AnalysisResult{Relevant: true, Sentiment: "neutral", RiskCategory: "normal_mention"}}, 70)
	require.NoError(t, e)
	svc := Service{Store: db, Directory: t.TempDir(), Threshold: 70}
	d, e := svc.Generate(ctx, "2026-10-06")
	require.NoError(t, e)
	require.Equal(t, 1, d.Total)
	csv, e := os.ReadFile(filepath.Join(svc.Directory, d.Date, "mentions.csv"))
	require.NoError(t, e)
	require.Contains(t, string(csv), "'=HYPERLINK")
	_, e = svc.Generate(ctx, "../invalid")
	require.Error(t, e)
	require.Equal(t, "'  +CMD", SafeCell("  +CMD"))
	require.Equal(t, "ordinary", SafeCell("ordinary"))
}
