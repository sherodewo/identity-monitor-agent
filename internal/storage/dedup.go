package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"golang.org/x/text/unicode/norm"
	"identity-monitor-agent/internal/extractor"
	"identity-monitor-agent/internal/model"
	"strings"
	"unicode"
)

// Preserve punctuation and emoji: they can change meaning. Only normalize
// equivalent Unicode, whitespace, and invisible formatting characters.
func contentFingerprint(m model.RawMention) string {
	text := strings.Map(func(r rune) rune {
		if r == '\u200b' || r == '\ufeff' || r == '\u2060' {
			return -1
		}
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, norm.NFKC.String(m.ContentText))
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return extractor.Hash(text)
}

func dedupKeys(m model.RawMention) []string {
	keys := []string{}
	if u, err := extractor.URL(m.SourceURL); err == nil {
		keys = append(keys, "url:"+u)
	}
	if hash := contentFingerprint(m); hash != "" {
		keys = append(keys, "text:"+hash)
	}
	return keys
}

func (s *SQLite) backfillDedup() error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS mention_dedup (subject_id TEXT NOT NULL, source TEXT NOT NULL, fingerprint TEXT NOT NULL, mention_id INTEGER NOT NULL REFERENCES mentions(id) ON DELETE CASCADE, PRIMARY KEY(subject_id,source,fingerprint))`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id,subject_id,data FROM mentions WHERE id NOT IN (SELECT mention_id FROM mention_dedup) ORDER BY id`)
	if err != nil {
		return err
	}
	var mentions []model.Mention
	for rows.Next() {
		var m model.Mention
		var raw string
		if err = rows.Scan(&m.ID, &m.SubjectID, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(raw), &m.RawMention); err != nil {
			rows.Close()
			return err
		}
		mentions = append(mentions, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range mentions {
		if err = insertDedup(context.Background(), tx, m.ID, m); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertDedup(ctx context.Context, tx *sql.Tx, id int64, m model.Mention) error {
	for _, key := range dedupKeys(m.RawMention) {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO mention_dedup VALUES(?,?,?,?)`, m.SubjectID, m.Source, key, id); err != nil {
			return err
		}
	}
	return nil
}
