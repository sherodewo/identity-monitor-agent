package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"identity-monitor-agent/internal/model"
)

type Execution struct {
	ID           string   `json:"id"`
	CreatedAt    string   `json:"created_at"`
	Label        string   `json:"label"`
	TotalQueries int      `json:"total_queries"`
	SubjectID    string   `json:"subject_id"`
	Source       string   `json:"source"`
	Queries      []string `json:"queries"`
	Completed    int      `json:"completed_queries"`
	Failed       int      `json:"failed_queries"`
}

func (s *SQLite) CreateExecution(ctx context.Context, id, label string, total int) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO executions VALUES(?,?,?,?)", id, now(), label, total)
	return err
}
func (s *SQLite) Executions(ctx context.Context) ([]Execution, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id,created_at,label,total_queries FROM executions ORDER BY created_at DESC,id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Execution{}
	for rows.Next() {
		var e Execution
		if err = rows.Scan(&e.ID, &e.CreatedAt, &e.Label, &e.TotalQueries); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		runs, err := s.DB.QueryContext(ctx, "SELECT subject_id,source,query,status FROM collection_runs WHERE execution_id=? ORDER BY rowid", out[i].ID)
		if err != nil {
			return nil, err
		}
		for runs.Next() {
			var subject, source, query, status string
			if err = runs.Scan(&subject, &source, &query, &status); err != nil {
				runs.Close()
				return nil, err
			}
			if out[i].SubjectID == "" {
				out[i].SubjectID, out[i].Source = subject, source
			}
			out[i].Queries = append(out[i].Queries, query)
			if status == "COMPLETED" {
				out[i].Completed++
			}
			if status == "FAILED" || status == "CANCELLED" || status == "HUMAN_ACTION_REQUIRED" {
				out[i].Failed++
			}
		}
		if err = runs.Err(); err != nil {
			runs.Close()
			return nil, err
		}
		if err = runs.Close(); err != nil {
			return nil, err
		}
		if len(out[i].Queries) > out[i].TotalQueries {
			out[i].TotalQueries = len(out[i].Queries)
		}
	}
	return out, nil
}
func linkMention(ctx context.Context, tx *sql.Tx, runID string, id int64) error {
	if runID == "" {
		return nil
	}
	var execution string
	if err := tx.QueryRowContext(ctx, "SELECT execution_id FROM collection_runs WHERE id=?", runID).Scan(&execution); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO execution_mentions VALUES(?,?)", execution, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO run_mentions VALUES(?,?)", runID, id)
	return err
}
func (s *SQLite) MentionRuns(ctx context.Context, id int64) ([]model.Run, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT r.data FROM collection_runs r JOIN run_mentions rm ON rm.run_id=r.id WHERE rm.mention_id=? ORDER BY r.rowid DESC", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		var r model.Run
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClearData removes only mentions exclusive to the selected execution. Shared
// mentions retain their analysis and cases in the other executions.
func (s *SQLite) ClearData(ctx context.Context, execution string) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if execution != "" {
		var exists int
		if err = tx.QueryRowContext(ctx, "SELECT 1 FROM executions WHERE id=?", execution).Scan(&exists); err != nil {
			return 0, err
		}
	}
	var busy int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM mentions WHERE analysis_status IN ('PENDING','SENTIMENT_RUNNING','CONTEXT_PENDING','ANALYZING')").Scan(&busy); err != nil {
		return 0, err
	}
	if busy > 0 {
		return 0, fmt.Errorf("tunggu antrean analisis selesai sebelum menghapus data")
	}
	if _, err = tx.ExecContext(ctx, "CREATE TEMP TABLE clear_mentions(id INTEGER PRIMARY KEY)"); err != nil {
		return 0, err
	}
	q := "INSERT INTO clear_mentions SELECT id FROM mentions"
	args := []any{}
	if execution != "" {
		q += " WHERE id IN (SELECT mention_id FROM execution_mentions WHERE execution_id=?) AND NOT EXISTS(SELECT 1 FROM execution_mentions em WHERE em.mention_id=mentions.id AND em.execution_id<>?)"
		args = []any{execution, execution}
	}
	if _, err = tx.ExecContext(ctx, q, args...); err != nil {
		return 0, err
	}
	for _, q = range []string{
		"DELETE FROM approvals WHERE case_id IN(SELECT id FROM cases WHERE mention_id IN(SELECT id FROM clear_mentions))",
		"DELETE FROM case_evidence WHERE case_id IN(SELECT id FROM cases WHERE mention_id IN(SELECT id FROM clear_mentions)) OR mention_id IN(SELECT id FROM clear_mentions)",
		"DELETE FROM cases WHERE mention_id IN(SELECT id FROM clear_mentions)",
	} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM mentions WHERE id IN(SELECT id FROM clear_mentions)")
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	where := ""
	args = nil
	if execution != "" {
		where = " WHERE execution_id=?"
		args = []any{execution}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM agent_steps WHERE run_id IN(SELECT id FROM collection_runs"+where+")", args...); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM collection_runs"+where, args...); err != nil {
		return 0, err
	}
	where = ""
	if execution != "" {
		where = " WHERE id=?"
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM executions"+where, args...); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "DROP TABLE clear_mentions"); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(entity,entity_id,action,actor,created_at) VALUES('execution',?,'CLEAR_DATA','local-reviewer',?)", execution, now()); err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

func (s *SQLite) ExecutionMentions(ctx context.Context, execution string) ([]model.Mention, error) {
	q, args := s.filter(model.MentionFilter{Execution: execution})
	rows, err := s.DB.QueryContext(ctx, "SELECT m.id,m.subject_id,m.content_hash,m.analysis_status,m.data,a.data"+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Mention{}
	for rows.Next() {
		m, e := scanMention(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
