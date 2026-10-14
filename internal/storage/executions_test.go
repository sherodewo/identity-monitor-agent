package storage

import (
	"context"
	"database/sql"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/migrations"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutionFilteringAndSafeClear(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.UpsertSubject(ctx, model.Subject{ID: "subject"}))
	for _, id := range []string{"a", "b"} {
		require.NoError(t, db.CreateRun(ctx, model.Run{ID: id, ExecutionID: id, SubjectID: "subject", Source: "test", Query: id, Status: "COMPLETED"}))
	}
	m := model.Mention{SubjectID: "subject", ContentHash: "shared", AnalysisStatus: "COMPLETED", RawMention: model.RawMention{Source: "test", SourceURL: "https://example.org/shared", ContentText: "Shared", CollectedAt: time.Now()}, Analysis: model.AnalysisResult{Sentiment: "negative", Relevant: true, RiskScore: 90, RiskCategory: "possible_scam"}}
	shared, dup, err := db.SaveMentionForRun(ctx, m, 70, "a")
	require.NoError(t, err)
	require.False(t, dup)
	id, dup, err := db.SaveMentionForRun(ctx, m, 70, "b")
	require.NoError(t, err)
	require.True(t, dup)
	require.Equal(t, shared, id)
	m.ContentHash = "exclusive"
	m.SourceURL = "https://example.org/exclusive"
	m.ContentText = "Exclusive"
	_, dup, err = db.SaveMentionForRun(ctx, m, 70, "a")
	require.NoError(t, err)
	require.False(t, dup)
	_, n, err := db.Mentions(ctx, model.MentionFilter{Execution: "a"})
	require.NoError(t, err)
	require.Equal(t, 2, n)
	_, n, err = db.Mentions(ctx, model.MentionFilter{Execution: "b"})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	runs, err := db.MentionRuns(ctx, shared)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.NoError(t, db.SetAnalysisStatus(ctx, shared, "PENDING"))
	_, err = db.ClearData(ctx, "a")
	require.Error(t, err)
	require.NoError(t, db.SetAnalysisStatus(ctx, shared, "COMPLETED"))
	deleted, err := db.ClearData(ctx, "a")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	_, err = db.Mention(ctx, shared)
	require.NoError(t, err)
	cases, err := db.Cases(ctx)
	require.NoError(t, err)
	require.Len(t, cases, 1)
	runs, err = db.MentionRuns(ctx, shared)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, "b", runs[0].ExecutionID)
	p, err := db.ExecutionProgress(ctx, "b")
	require.NoError(t, err)
	require.Equal(t, 1, p.Total)
	require.Equal(t, 1, p.Completed)
	deleted, err = db.ClearData(ctx, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	p, err = db.Progress(ctx)
	require.NoError(t, err)
	require.Zero(t, p.Total)
	subjects, err := db.Subjects(ctx)
	require.NoError(t, err)
	require.Len(t, subjects, 1)
	executions, err := db.Executions(ctx)
	require.NoError(t, err)
	require.Empty(t, executions)
	// Foreign keys, fingerprints, and temporary tables must not block a fresh run.
	require.NoError(t, db.CreateRun(ctx, model.Run{ID: "c", SubjectID: "subject", Source: "test", Status: "COMPLETED"}))
	_, dup, err = db.SaveMentionForRun(ctx, m, 70, "c")
	require.NoError(t, err)
	require.False(t, dup)
}

func TestLegacyExecutionMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = raw.Exec("CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL)")
	require.NoError(t, err)
	for _, name := range []string{"001_initial.sql", "002_media_dedup.sql"} {
		b, err := migrations.Files.ReadFile(name)
		require.NoError(t, err)
		_, err = raw.Exec(string(b))
		require.NoError(t, err)
		_, err = raw.Exec("INSERT INTO schema_migrations VALUES(?,?)", name, now())
		require.NoError(t, err)
	}
	_, err = raw.Exec(`INSERT INTO subjects VALUES('s','{"id":"s"}'); INSERT INTO mentions(subject_id,source,canonical_url,content_hash,collected_at,data,analysis_status) VALUES('s','test','https://example.org','h','2026-01-01','{"source":"test","source_url":"https://example.org","content_text":"Legacy"}','COMPLETED'); INSERT INTO analysis_results VALUES(1,0,'neutral','normal_mention',0,'{"sentiment":"neutral"}')`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	db, err := Open(path)
	require.NoError(t, err)
	defer db.Close()
	mentions, n, err := db.Mentions(context.Background(), model.MentionFilter{Execution: "legacy"})
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, "Legacy", mentions[0].ContentText)
}
