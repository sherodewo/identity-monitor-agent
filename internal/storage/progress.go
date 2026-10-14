package storage

import "context"

type StageProgress struct {
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
}
type Progress struct {
	Sentiment        StageProgress `json:"sentiment"`
	Context          StageProgress `json:"context"`
	Queries          int           `json:"queries"`
	QueriesCompleted int           `json:"queries_completed"`
	QueriesFailed    int           `json:"queries_failed"`
	QueriesActive    int           `json:"queries_active"`
	Total            int           `json:"total"`
	Completed        int           `json:"completed"`
	Failed           int           `json:"failed"`
	Pending          int           `json:"pending"`
	Analyzing        int           `json:"analyzing"`
	Discovered       int           `json:"discovered"`
	Duplicates       int           `json:"duplicates"`
}

func (s *SQLite) Progress(ctx context.Context) (Progress, error) { return s.ExecutionProgress(ctx, "") }
func (s *SQLite) ExecutionProgress(ctx context.Context, execution string) (Progress, error) {
	p := Progress{}
	where := ""
	args := []any{}
	if execution != "" {
		where = " WHERE id IN(SELECT mention_id FROM execution_mentions WHERE execution_id=?)"
		args = append(args, execution)
	}
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(analysis_status='COMPLETED'),0), COALESCE(SUM(analysis_status='ANALYSIS_FAILED'),0), COALESCE(SUM(analysis_status IN ('PENDING','CONTEXT_PENDING')),0), COALESCE(SUM(analysis_status IN ('SENTIMENT_RUNNING','ANALYZING')),0) FROM mentions`+where, args...).Scan(&p.Total, &p.Completed, &p.Failed, &p.Pending, &p.Analyzing)
	if err != nil {
		return p, err
	}

	stageWhere := ""
	stageArgs := []any{}
	if execution != "" {
		stageWhere = " WHERE m.id IN(SELECT mention_id FROM execution_mentions WHERE execution_id=?)"
		stageArgs = append(stageArgs, execution)
	}
	stageQuery := `SELECT COALESCE(SUM(ss='COMPLETED'),0),COALESCE(SUM(ss='FAILED'),0),COALESCE(SUM(ss='PENDING'),0),COALESCE(SUM(ss='RUNNING'),0),COALESCE(SUM(cs='COMPLETED'),0),COALESCE(SUM(cs='FAILED'),0),COALESCE(SUM(cs='PENDING'),0),COALESCE(SUM(cs='RUNNING'),0) FROM (SELECT
 COALESCE(json_extract(a.data,'$.sentiment_status'),CASE WHEN m.analysis_status='COMPLETED' THEN 'COMPLETED' WHEN m.analysis_status='ANALYSIS_FAILED' THEN CASE WHEN COALESCE(json_extract(a.data,'$.sentiment_model'),'') NOT IN ('','indobert:error') THEN 'COMPLETED' ELSE 'FAILED' END WHEN m.analysis_status='SENTIMENT_RUNNING' THEN 'RUNNING' ELSE 'PENDING' END) ss,
 CASE WHEN m.analysis_status='ANALYZING' THEN 'RUNNING' WHEN m.analysis_status='COMPLETED' THEN 'COMPLETED' WHEN m.analysis_status='ANALYSIS_FAILED' THEN COALESCE(json_extract(a.data,'$.context_status'),'FAILED') ELSE 'PENDING' END cs
 FROM mentions m JOIN analysis_results a ON a.mention_id=m.id` + stageWhere + `)`
	if err = s.DB.QueryRowContext(ctx, stageQuery, stageArgs...).Scan(&p.Sentiment.Completed, &p.Sentiment.Failed, &p.Sentiment.Pending, &p.Sentiment.Running, &p.Context.Completed, &p.Context.Failed, &p.Context.Pending, &p.Context.Running); err != nil {
		return p, err
	}
	runs, err := s.Runs(ctx)
	for _, r := range runs {
		if execution != "" && r.ExecutionID != execution {
			continue
		}
		p.Queries++
		switch r.Status {
		case "COMPLETED":
			p.QueriesCompleted++
		case "PENDING", "RUNNING":
			p.QueriesActive++
		default:
			p.QueriesFailed++
		}
		p.Discovered += r.TotalDiscovered
		p.Duplicates += r.TotalDuplicate
	}
	if err != nil {
		return p, err
	}
	query := "SELECT COALESCE(SUM(total_queries),0) FROM executions"
	args = nil
	if execution != "" {
		query += " WHERE id=?"
		args = []any{execution}
	}
	var planned int
	if err = s.DB.QueryRowContext(ctx, query, args...).Scan(&planned); err != nil {
		return p, err
	}
	if planned > p.Queries {
		p.Queries = planned
	}
	return p, nil
}
