package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/migrations"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SQLite struct {
	DB       *sql.DB
	Location *time.Location
}

func Open(path string) (*SQLite, error) {
	if path != ":memory:" {
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return nil, e
		}
		f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return nil, e
		}
		_ = f.Close()
		if e = os.Chmod(path, 0600); e != nil {
			return nil, e
		}
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &SQLite{DB: db, Location: time.FixedZone("WIB", 7*3600)}
	for _, q := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", "PRAGMA busy_timeout=5000"} {
		if _, e = db.Exec(q); e != nil {
			_ = db.Close()
			return nil, e
		}
	}
	if e = s.migrate(); e != nil {
		_ = db.Close()
		return nil, e
	}
	return s, nil
}
func (s *SQLite) Close() error { return s.DB.Close() }
func (s *SQLite) migrate() error {
	if _, e := s.DB.Exec("CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)"); e != nil {
		return e
	}
	files, e := migrations.Files.ReadDir(".")
	if e != nil {
		return e
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".sql") {
			continue
		}
		var n int
		if e = s.DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", f.Name()).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			continue
		}
		b, e := migrations.Files.ReadFile(f.Name())
		if e != nil {
			return e
		}
		tx, e := s.DB.Begin()
		if e != nil {
			return e
		}
		if _, e = tx.Exec(string(b)); e == nil {
			_, e = tx.Exec("INSERT INTO schema_migrations VALUES (?,?)", f.Name(), now())
		}
		if e != nil {
			_ = tx.Rollback()
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	return nil
}
func now() string         { return time.Now().UTC().Format(time.RFC3339Nano) }
func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
func (s *SQLite) UpsertSubject(ctx context.Context, v model.Subject) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = tx.ExecContext(ctx, "INSERT INTO subjects VALUES (?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", v.ID, encode(v)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM subject_aliases WHERE subject_id=?", v.ID); e != nil {
		return e
	}
	for _, a := range v.Aliases {
		if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO subject_aliases VALUES (?,?)", v.ID, a); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *SQLite) Subjects(ctx context.Context) ([]model.Subject, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT data FROM subjects ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Subject{}
	for rows.Next() {
		var b string
		var v model.Subject
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *SQLite) SaveMention(ctx context.Context, m model.Mention, threshold int) (int64, bool, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, false, e
	}
	defer func() { _ = tx.Rollback() }()
	res, e := tx.ExecContext(ctx, "INSERT INTO mentions(subject_id,source,canonical_url,content_hash,collected_at,data,analysis_status) VALUES (?,?,?,?,?,?,?) ON CONFLICT DO NOTHING", m.SubjectID, m.Source, m.SourceURL, m.ContentHash, m.CollectedAt.UTC().Format(time.RFC3339Nano), encode(m.RawMention), m.AnalysisStatus)
	if e != nil {
		return 0, false, e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return 0, false, e
	}
	if n == 0 {
		return 0, true, nil
	}
	id, e := res.LastInsertId()
	if e != nil {
		return 0, false, e
	}
	a := m.Analysis
	if _, e = tx.ExecContext(ctx, "INSERT INTO analysis_results VALUES (?,?,?,?,?,?)", id, a.RiskScore, a.Sentiment, a.RiskCategory, a.RequiresReview, encode(a)); e != nil {
		return 0, false, e
	}
	for _, url := range m.MediaURLs {
		if _, e = tx.ExecContext(ctx, "INSERT INTO mention_media(mention_id,url) VALUES (?,?)", id, url); e != nil {
			return 0, false, e
		}
	}
	if a.Relevant && (a.RiskScore >= threshold || strings.Contains("possible_impersonation possible_scam possible_doxxing possible_threat", a.RiskCategory) && a.RiskCategory != "") {
		if _, e = createCase(ctx, tx, id, "system"); e != nil {
			return 0, false, e
		}
	}
	return id, false, tx.Commit()
}
func (s *SQLite) filter(f model.MentionFilter) (string, []any) {
	q := " FROM mentions m JOIN analysis_results a ON a.mention_id=m.id WHERE 1=1"
	args := []any{}
	for _, p := range []struct{ k, v string }{{"m.subject_id", f.Subject}, {"m.source", f.Source}, {"a.sentiment", f.Sentiment}, {"a.category", f.Category}} {
		if p.v != "" {
			q += " AND " + p.k + "=?"
			args = append(args, p.v)
		}
	}
	if f.Date != "" {
		t, _ := time.ParseInLocation("2006-01-02", f.Date, s.Location)
		q += " AND m.collected_at>=? AND m.collected_at<?"
		args = append(args, t.UTC().Format(time.RFC3339Nano), t.AddDate(0, 0, 1).UTC().Format(time.RFC3339Nano))
	}
	if f.MinRisk > 0 {
		q += " AND a.risk_score>=?"
		args = append(args, f.MinRisk)
	}
	if f.Review {
		q += " AND (a.requires_review=1 OR m.analysis_status='ANALYSIS_FAILED')"
	}
	return q, args
}
func (s *SQLite) Mentions(ctx context.Context, f model.MentionFilter) ([]model.Mention, int, error) {
	q, args := s.filter(f)
	var count int
	if e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*)"+q, args...).Scan(&count); e != nil {
		return nil, 0, e
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 50
	}
	args = append(args, f.Limit, f.Offset)
	rows, e := s.DB.QueryContext(ctx, "SELECT m.id,m.subject_id,m.content_hash,m.analysis_status,m.data,a.data"+q+" ORDER BY m.id DESC LIMIT ? OFFSET ?", args...)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	out := []model.Mention{}
	for rows.Next() {
		m, e := scanMention(rows)
		if e != nil {
			return nil, 0, e
		}
		out = append(out, m)
	}
	return out, count, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanMention(row scanner) (model.Mention, error) {
	var m model.Mention
	var raw, a string
	e := row.Scan(&m.ID, &m.SubjectID, &m.ContentHash, &m.AnalysisStatus, &raw, &a)
	if e != nil {
		return m, e
	}
	if e = json.Unmarshal([]byte(raw), &m.RawMention); e != nil {
		return m, e
	}
	e = json.Unmarshal([]byte(a), &m.Analysis)
	return m, e
}
func (s *SQLite) Mention(ctx context.Context, id int64) (model.Mention, error) {
	return scanMention(s.DB.QueryRowContext(ctx, "SELECT m.id,m.subject_id,m.content_hash,m.analysis_status,m.data,a.data FROM mentions m JOIN analysis_results a ON a.mention_id=m.id WHERE m.id=?", id))
}
func (s *SQLite) CreateRun(ctx context.Context, r model.Run) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	if _, e = tx.ExecContext(ctx, "INSERT INTO collection_runs VALUES (?,?,?,?,?,?)", r.ID, r.SubjectID, r.Source, r.Query, r.Status, encode(r)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO search_tasks(id,status) VALUES (?,?)", r.ID, r.Status); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *SQLite) UpdateRun(ctx context.Context, r model.Run) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	var old string
	if e = tx.QueryRowContext(ctx, "SELECT status FROM collection_runs WHERE id=?", r.ID).Scan(&old); e != nil {
		return e
	}
	if old != r.Status && !model.RunTransition(old, r.Status) {
		return fmt.Errorf("invalid run transition %s -> %s", old, r.Status)
	}
	if _, e = tx.ExecContext(ctx, "UPDATE collection_runs SET status=?,data=? WHERE id=?", r.Status, encode(r), r.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE search_tasks SET checkpoint=?,status=? WHERE id=?", r.TotalStored+r.TotalDuplicate+r.TotalFailed, r.Status, r.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *SQLite) Runs(ctx context.Context) ([]model.Run, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT data FROM collection_runs ORDER BY rowid DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		var b string
		var r model.Run
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &r); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func createCase(ctx context.Context, tx *sql.Tx, id int64, actor string) (model.Case, error) {
	c := model.Case{MentionID: id, Status: "OPEN", CreatedAt: time.Now().UTC()}
	res, e := tx.ExecContext(ctx, "INSERT INTO cases(mention_id,status,created_at) VALUES (?,?,?)", id, c.Status, c.CreatedAt.Format(time.RFC3339Nano))
	if e != nil {
		return c, e
	}
	c.ID, e = res.LastInsertId()
	if e != nil {
		return c, e
	}
	var data string
	if e = tx.QueryRowContext(ctx, "SELECT data FROM mentions WHERE id=?", id).Scan(&data); e != nil {
		return c, e
	}
	var raw model.RawMention
	if e = json.Unmarshal([]byte(data), &raw); e != nil {
		return c, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO case_evidence VALUES (?,?,?)", c.ID, id, raw.ScreenshotPath); e != nil {
		return c, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO audit_logs(entity,entity_id,action,actor,created_at) VALUES ('case',?,'OPEN',?,?)", strconv.FormatInt(c.ID, 10), actor, now())
	return c, e
}
func (s *SQLite) CreateCase(ctx context.Context, id int64, actor string) (model.Case, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return model.Case{}, e
	}
	defer func() { _ = tx.Rollback() }()
	c, e := createCase(ctx, tx, id, actor)
	if e != nil {
		return c, e
	}
	return c, tx.Commit()
}
func (s *SQLite) Cases(ctx context.Context) ([]model.Case, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id,mention_id,status,created_at FROM cases ORDER BY id DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Case{}
	for rows.Next() {
		var c model.Case
		var t string
		if e = rows.Scan(&c.ID, &c.MentionID, &c.Status, &t); e != nil {
			return nil, e
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, t)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *SQLite) TransitionCase(ctx context.Context, id int64, to, actor string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback() }()
	var from string
	if e = tx.QueryRowContext(ctx, "SELECT status FROM cases WHERE id=?", id).Scan(&from); e != nil {
		return e
	}
	if !model.CaseTransition(from, to) {
		return fmt.Errorf("invalid case transition %s -> %s", from, to)
	}
	if _, e = tx.ExecContext(ctx, "UPDATE cases SET status=? WHERE id=?", to, id); e != nil {
		return e
	}
	if to == "APPROVED" || to == "REJECTED" || to == "WAITING_APPROVAL" {
		if _, e = tx.ExecContext(ctx, "INSERT INTO approvals(case_id,decision,actor,created_at) VALUES (?,?,?,?)", id, to, actor, now()); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO audit_logs(entity,entity_id,action,actor,created_at) VALUES ('case',?,?,?,?)", strconv.FormatInt(id, 10), to, actor, now()); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *SQLite) Audit(ctx context.Context, entity, id, action, actor string) error {
	_, e := s.DB.ExecContext(ctx, "INSERT INTO audit_logs(entity,entity_id,action,actor,created_at) VALUES (?,?,?,?,?)", entity, id, action, actor, now())
	return e
}
func (s *SQLite) CaseAudit(ctx context.Context, id int64) ([]model.Audit, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT id,entity,entity_id,action,actor,created_at FROM audit_logs WHERE entity='case' AND entity_id=? ORDER BY id", strconv.FormatInt(id, 10))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Audit{}
	for rows.Next() {
		var a model.Audit
		if e = rows.Scan(&a.ID, &a.Entity, &a.EntityID, &a.Action, &a.Actor, &a.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *SQLite) Event(ctx context.Context, kind, detail string) error {
	_, e := s.DB.ExecContext(ctx, "INSERT INTO system_events(kind,detail,created_at) VALUES (?,?,?)", kind, detail, now())
	return e
}
func (s *SQLite) Retain(ctx context.Context, days int) error {
	_, e := s.DB.ExecContext(ctx, "DELETE FROM mentions WHERE collected_at<? AND id NOT IN (SELECT mention_id FROM cases)", time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano))
	return e
}
func (s *SQLite) AllMentions(ctx context.Context, date string) ([]model.Mention, error) {
	out := []model.Mention{}
	for offset := 0; ; {
		v, n, e := s.Mentions(ctx, model.MentionFilter{Date: date, Limit: 1000, Offset: offset})
		if e != nil {
			return nil, e
		}
		out = append(out, v...)
		offset += len(v)
		if offset >= n {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
