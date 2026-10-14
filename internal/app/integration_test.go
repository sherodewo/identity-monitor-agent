package app

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/model"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runtime(t *testing.T) *Runtime {
	t.Helper()
	c := config.Defaults()
	dir := t.TempDir()
	c.Demo = true
	c.Scheduler.Enabled = false
	c.App.DataDirectory = filepath.Join(dir, "data")
	c.App.ReportDirectory = filepath.Join(dir, "reports")
	c.App.EvidenceDirectory = filepath.Join(dir, "evidence")
	r, e := Open(context.Background(), c, "../../configs/subjects.example.yaml", true)
	require.NoError(t, e)
	t.Cleanup(r.Close)
	return r
}
func TestFixtureToDashboard(t *testing.T) {
	r := runtime(t)
	ctx := context.Background()
	require.NoError(t, r.Demo(ctx))
	mentions, n, e := r.Store.Mentions(ctx, model.MentionFilter{})
	require.NoError(t, e)
	require.Equal(t, 4, n)
	require.Len(t, mentions, 4)
	_, review, e := r.Store.Mentions(ctx, model.MentionFilter{Review: true})
	require.NoError(t, e)
	require.Equal(t, 1, review)
	runs, e := r.Store.Runs(ctx)
	require.NoError(t, e)
	require.Equal(t, 3, runs[0].TotalDuplicate)
	resp, e := r.Dashboard.App.Test(httptest.NewRequest("GET", "http://localhost/api/v1/mentions", nil))
	require.NoError(t, e)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
	var result struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Equal(t, 4, result.Total)
	for _, path := range []string{"/", "/static/app.js", "/healthz", "/readyz", "/api/v1/review-queue", "/api/v1/dashboard/summary", "/api/v1/progress", "/api/v1/cases/1", "/api/v1/reports/daily.csv"} {
		response, e := r.Dashboard.App.Test(httptest.NewRequest("GET", "http://localhost"+path, nil), 5000)
		require.NoError(t, e, path)
		require.Equal(t, 200, response.StatusCode, path)
		_ = response.Body.Close()
	}
	for _, action := range []string{"request-approval", "approve", "mark-submitted", "mark-removed", "close"} {
		req := httptest.NewRequest("POST", "http://localhost/api/v1/cases/1/"+action, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Local-Request", "1")
		resp, e := r.Dashboard.App.Test(req)
		require.NoError(t, e)
		require.Equal(t, 200, resp.StatusCode, action)
		_ = resp.Body.Close()
	}
}
func TestAPISecurity(t *testing.T) {
	r := runtime(t)
	for _, v := range []struct {
		method, url, origin string
		status              int
	}{{"GET", "http://evil.example/api/v1/subjects", "", 403}, {"POST", "http://localhost/api/v1/runs", "", 403}, {"GET", "http://localhost/api/v1/subjects", "http://evil.example", 403}, {"GET", "http://localhost/api/v1/mentions?limit=-1", "", 400}, {"GET", "http://localhost/api/v1/mentions?date=bad", "", 400}, {"GET", "http://localhost/api/v1/mentions/9999", "", 404}} {
		req := httptest.NewRequest(v.method, v.url, nil)
		req.Header.Set("Origin", v.origin)
		resp, e := r.Dashboard.App.Test(req)
		require.NoError(t, e)
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.Equal(t, v.status, resp.StatusCode, string(b))
	}
	r.Dashboard.Config.App.APIToken = "secret"
	resp, e := r.Dashboard.App.Test(httptest.NewRequest("GET", "http://localhost/api/v1/subjects", nil))
	require.NoError(t, e)
	require.Equal(t, 401, resp.StatusCode)
	_ = resp.Body.Close()
}

type blockedSource struct{}

func (blockedSource) Name() string                                     { return "generic_web_search" }
func (blockedSource) CheckSession(context.Context) model.SessionStatus { return model.SessionHuman }
func (blockedSource) Search(context.Context, model.SearchTask) ([]model.RawMention, error) {
	return nil, guardrail.ErrHuman
}
func TestHumanActionRun(t *testing.T) {
	r := runtime(t)
	r.Supervisor.Sources["generic_web_search"] = blockedSource{}
	run, e := r.Supervisor.Submit(context.Background(), "subject-001", "generic_web_search", "")
	require.NoError(t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, e = r.Supervisor.Wait(ctx, run.ID)
	require.NoError(t, e)
	require.Equal(t, "HUMAN_ACTION_REQUIRED", run.Status)
	require.Zero(t, run.TotalStored)
}

func TestExecutionScopeAndClearAPI(t *testing.T) {
	r := runtime(t)
	ctx := context.Background()
	require.NoError(t, r.Demo(ctx))
	executions, err := r.Store.Executions(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 3)
	execution := executions[0].ID
	for _, path := range []string{"/api/v1/mentions?execution=" + execution, "/api/v1/runs?execution=" + execution, "/api/v1/progress?execution=" + execution, "/api/v1/dashboard/summary?execution=" + execution, "/api/v1/cases?execution=" + execution, "/api/v1/executions", "/api/v1/mentions/1/runs"} {
		response, err := r.Dashboard.App.Test(httptest.NewRequest("GET", "http://localhost"+path, nil), 5000)
		require.NoError(t, err)
		require.Equal(t, 200, response.StatusCode, path)
		response.Body.Close()
	}
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"execution":"` + execution + `"}`, 400},
		{`{"execution":"` + execution + `","confirmation":"HAPUS"}`, 200},
	} {
		request := httptest.NewRequest("POST", "http://localhost/api/v1/data/clear", strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Local-Request", "1")
		response, err := r.Dashboard.App.Test(request)
		require.NoError(t, err)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		require.Equal(t, test.status, response.StatusCode, string(body))
	}
	// The last demo run contained only duplicates: removing it preserves all posts.
	_, n, err := r.Store.Mentions(ctx, model.MentionFilter{})
	require.NoError(t, err)
	require.Equal(t, 4, n)
}

func TestIrrelevantExcludedFromDashboardAndReport(t *testing.T) {
	r := runtime(t)
	ctx := context.Background()
	require.NoError(t, r.Demo(ctx))
	r.Supervisor.Stop() // Freeze the test fixture before adding pending examples.
	subject := "subject-001"
	at := time.Now()
	for _, v := range []struct{ key, status, contextStatus string }{{"irrelevant", "COMPLETED", "COMPLETED"}, {"waiting", "CONTEXT_PENDING", "PENDING"}, {"failure", "ANALYSIS_FAILED", "FAILED"}} {
		_, _, err := r.Store.SaveMention(ctx, model.Mention{SubjectID: subject, ContentHash: v.key, AnalysisStatus: v.status, RawMention: model.RawMention{Source: "test", SourceURL: "https://example.org/" + v.key, ContentText: v.key, CollectedAt: at}, Analysis: model.AnalysisResult{Sentiment: "negative", SentimentStatus: "COMPLETED", ContextStatus: v.contextStatus, Relevant: false, Summary: "Not the monitored subject"}}, 70)
		require.NoError(t, err)
	}
	response, err := r.Dashboard.App.Test(httptest.NewRequest("GET", "http://localhost/api/v1/dashboard/summary", nil), 5000)
	require.NoError(t, err)
	defer response.Body.Close()
	var summary struct {
		Count     int            `json:"mentions_today"`
		Excluded  int            `json:"excluded_mentions"`
		Pending   int            `json:"pending_relevance"`
		Sentiment map[string]int `json:"sentiment"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&summary))
	require.Equal(t, 4, summary.Count)
	require.Equal(t, 1, summary.Excluded)
	require.Equal(t, 2, summary.Pending)
	require.Equal(t, 2, summary.Sentiment["negative"])
	for _, v := range []struct {
		filter string
		want   int
	}{{"", 6}, {"?relevance=excluded", 1}, {"?relevance=relevant", 4}, {"?relevance=all", 7}, {"?relevance=pending", 2}} {
		resp, err := r.Dashboard.App.Test(httptest.NewRequest("GET", "http://localhost/api/v1/mentions"+v.filter, nil))
		require.NoError(t, err)
		var result struct {
			Total int `json:"total"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		resp.Body.Close()
		require.Equal(t, v.want, result.Total, v.filter)
	}
	report, err := r.Reports.Generate(ctx, at.In(r.Store.Location).Format("2006-01-02"))
	require.NoError(t, err)
	require.Equal(t, 4, report.Total)
	require.Equal(t, 1, report.Excluded)
	require.Equal(t, 2, report.PendingRelevance)
}
