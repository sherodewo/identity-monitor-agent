package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"identity-monitor-agent/internal/model"
	"strconv"
	"strings"
	"time"
)

const contextDoneSQL = `(m.analysis_status='COMPLETED' OR (m.analysis_status='ANALYSIS_FAILED' AND COALESCE(json_extract(a.data,'$.context_status'),'')='COMPLETED'))`
const manualRelevanceSQL = `COALESCE(json_extract(a.data,'$.manual_relevance'),'')`
const excludedSQL = `(` + manualRelevanceSQL + `='excluded' OR (` + manualRelevanceSQL + `='' AND ` + contextDoneSQL + ` AND COALESCE(json_extract(a.data,'$.relevant'),0)=0))`

func (s *SQLite) ReviewRelevance(ctx context.Context, id int64, decision, actor string) error {
	if decision != "relevant" && decision != "excluded" {
		return fmt.Errorf("invalid relevance decision")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, "SELECT data FROM analysis_results WHERE mention_id=?", id).Scan(&raw); err != nil {
		return err
	}
	var analysis model.AnalysisResult
	if err = json.Unmarshal([]byte(raw), &analysis); err != nil {
		return err
	}
	analysis.ManualRelevance = decision
	analysis.Relevant = decision == "relevant"
	analysis.RequiresReview = false
	if _, err = tx.ExecContext(ctx, "UPDATE analysis_results SET requires_review=0,data=? WHERE mention_id=?", encode(analysis), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO audit_logs(entity,entity_id,action,actor,created_at) VALUES('mention',?,?,?,?)", strconv.FormatInt(id, 10), "RELEVANCE_"+strings.ToUpper(decision), actor, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLite) CasesFiltered(ctx context.Context, f model.MentionFilter) ([]model.Case, error) {
	q, args := s.filter(f)
	rows, err := s.DB.QueryContext(ctx, "SELECT id,mention_id,status,created_at FROM cases WHERE mention_id IN (SELECT m.id"+q+") ORDER BY id DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Case{}
	for rows.Next() {
		var c model.Case
		var at string
		if err = rows.Scan(&c.ID, &c.MentionID, &c.Status, &at); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, c)
	}
	return out, rows.Err()
}
