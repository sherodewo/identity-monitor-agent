package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"identity-monitor-agent/internal/model"
	"os"
	"path/filepath"
	"time"
)

func (s *SQLite) Duplicate(ctx context.Context, subject, source, url, hash string) (bool, error) {
	var n int
	e := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM mentions WHERE subject_id=? AND source=? AND (canonical_url=? OR content_hash=?)", subject, source, url, hash).Scan(&n)
	return n > 0, e
}
func (s *SQLite) SourceConfig(ctx context.Context, name string, v any) error {
	_, e := s.DB.ExecContext(ctx, "INSERT INTO source_configs(name,data) VALUES (?,?) ON CONFLICT(name) DO UPDATE SET data=excluded.data", name, encode(v))
	return e
}

// Archive old audit rows without mutating or deleting the append-only source journal.
// Archives are local, permission-restricted copies, not external uploads.
func (s *SQLite) ArchiveAudit(ctx context.Context, dir string, days int) error {
	cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
	rows, e := s.DB.QueryContext(ctx, "SELECT id,entity,entity_id,action,actor,created_at FROM audit_logs WHERE created_at<? ORDER BY id", cutoff)
	if e != nil {
		return e
	}
	defer rows.Close()
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	path := filepath.Join(dir, "audit-before-"+time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")+".jsonl")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	for rows.Next() {
		var a model.Audit
		if e = rows.Scan(&a.ID, &a.Entity, &a.EntityID, &a.Action, &a.Actor, &a.CreatedAt); e != nil {
			return e
		}
		if e = encoder.Encode(a); e != nil {
			return e
		}
	}
	if e = rows.Err(); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return fmt.Errorf("sync audit archive: %w", e)
	}
	return nil
}
