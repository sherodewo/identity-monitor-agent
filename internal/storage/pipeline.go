package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/sentiment"
)

func (s *SQLite) PendingAnalysisCount(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM mentions WHERE analysis_status IN ('PENDING','SENTIMENT_RUNNING','CONTEXT_PENDING','ANALYZING')").Scan(&n)
	return n, err
}

func fillStageStatus(m *model.Mention) {
	a := &m.Analysis
	if a.SentimentStatus == "" {
		switch m.AnalysisStatus {
		case "SENTIMENT_RUNNING":
			a.SentimentStatus = "RUNNING"
		case "COMPLETED":
			a.SentimentStatus = "COMPLETED"
		case "ANALYSIS_FAILED":
			if a.SentimentModel != "" && a.SentimentModel != "indobert:error" {
				a.SentimentStatus = "COMPLETED"
			} else {
				a.SentimentStatus = "FAILED"
			}
		default:
			a.SentimentStatus = "PENDING"
		}
	}
	switch m.AnalysisStatus {
	case "ANALYZING":
		a.ContextStatus = "RUNNING"
	case "COMPLETED":
		a.ContextStatus = "COMPLETED"
	case "ANALYSIS_FAILED":
		if a.ContextStatus == "" {
			a.ContextStatus = "FAILED"
		}
	default:
		a.ContextStatus = "PENDING"
	}
}

// Claim one row at a time. SQLite holds the entire backlog, so a slow context
// worker cannot block the sentiment worker or the browser with a full channel.
func (s *SQLite) ClaimAnalysis(ctx context.Context, stage string, split bool) (model.Mention, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return model.Mention{}, err
	}
	defer tx.Rollback()
	where := "m.analysis_status='CONTEXT_PENDING'"
	running := "ANALYZING"
	if stage == "sentiment" {
		where = "m.analysis_status='PENDING'"
		running = "SENTIMENT_RUNNING"
	} else if !split {
		where = "m.analysis_status IN ('PENDING','CONTEXT_PENDING')"
	}
	m, err := scanMention(tx.QueryRowContext(ctx, "SELECT m.id,m.subject_id,m.content_hash,m.analysis_status,m.data,a.data FROM mentions m JOIN analysis_results a ON a.mention_id=m.id WHERE "+where+" ORDER BY m.id LIMIT 1"))
	if err != nil {
		return m, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mentions SET analysis_status=? WHERE id=?", running, m.ID); err != nil {
		return m, err
	}
	m.AnalysisStatus = running
	return m, tx.Commit()
}

func (s *SQLite) RecoverAnalysis(ctx context.Context) error {
	// Only called once before the workers start. Committed sentiment is retained.
	_, err := s.DB.ExecContext(ctx, `UPDATE mentions SET analysis_status=CASE WHEN analysis_status='SENTIMENT_RUNNING' THEN 'PENDING' WHEN EXISTS(SELECT 1 FROM analysis_results a WHERE a.mention_id=mentions.id AND json_extract(a.data,'$.sentiment_status') IN ('COMPLETED','FAILED')) THEN 'CONTEXT_PENDING' ELSE 'PENDING' END WHERE analysis_status IN ('SENTIMENT_RUNNING','ANALYZING')`)
	return err
}

func (s *SQLite) ReleaseAnalysis(ctx context.Context, id int64, stage string, split bool) error {
	from, to := "ANALYZING", "CONTEXT_PENDING"
	if stage == "sentiment" {
		from, to = "SENTIMENT_RUNNING", "PENDING"
	} else if !split {
		to = "PENDING"
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE mentions SET analysis_status=? WHERE id=? AND analysis_status=?", to, id, from)
	return err
}

func (s *SQLite) CompleteSentiment(ctx context.Context, id int64, result sentiment.Result, failed bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var data, status string
	if err = tx.QueryRowContext(ctx, "SELECT a.data,m.analysis_status FROM analysis_results a JOIN mentions m ON m.id=a.mention_id WHERE m.id=?", id).Scan(&data, &status); err != nil {
		return err
	}
	if status != "SENTIMENT_RUNNING" {
		return fmt.Errorf("sentiment job %d is no longer active", id)
	}
	var a model.AnalysisResult
	if err = json.Unmarshal([]byte(data), &a); err != nil {
		return err
	}
	a.Sentiment = result.Label
	a.SentimentScore = result.Score
	a.SentimentConfidence = result.Confidence
	a.SentimentModel = result.Model
	a.SentimentStatus = "COMPLETED"
	a.ContextStatus = "PENDING"
	if failed {
		a.Sentiment = "unknown"
		a.SentimentStatus = "FAILED"
		a.RequiresReview = true
		a.Reasons = append(a.Reasons, "IndoBERT sentiment analysis failed")
	}
	if result.Label == "unknown" && !failed {
		a.RequiresReview = true
		a.Reasons = append(a.Reasons, "IndoBERT sentiment confidence below threshold")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE analysis_results SET sentiment=?,requires_review=?,data=? WHERE mention_id=?", a.Sentiment, a.RequiresReview, encode(a), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE mentions SET analysis_status='CONTEXT_PENDING' WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
