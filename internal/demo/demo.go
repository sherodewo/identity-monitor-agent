package demo

import (
	"context"
	"embed"
	"encoding/json"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/ollama"
	"identity-monitor-agent/internal/source"
	"net/http"
	"net/http/httptest"
	"strings"
)

//go:embed fixtures/*.html
var Fixtures embed.FS

type Adapter struct{ Source string }

func (a Adapter) Name() string                                     { return a.Source }
func (a Adapter) CheckSession(context.Context) model.SessionStatus { return model.SessionReady }
func (a Adapter) Search(_ context.Context, t model.SearchTask) ([]model.RawMention, error) {
	name := "generic"
	base := "https://www.google.com/search?q=fixture"
	switch a.Source {
	case "youtube_public_search":
		name = "youtube"
		base = "https://www.youtube.com/results?search_query=fixture"
	case "threads_public_search":
		name = "threads"
		base = "https://www.threads.com/search?q=fixture"
	}
	b, e := Fixtures.ReadFile("fixtures/" + name + ".html")
	if e != nil {
		return nil, e
	}
	return source.Extract(a.Source, base, string(b), t.MaxResults)
}

// Server is explicitly synthetic and exercises the real local HTTP Ollama client.
func Server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/tags" {
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]string{"name": "fixture-mock"}}})
			return
		}
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Messages []ollama.Message `json:"messages"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in) != nil || len(in.Messages) < 2 {
			w.WriteHeader(400)
			return
		}
		var payload struct {
			Text string `json:"untrusted_page"`
		}
		_ = json.Unmarshal([]byte(in.Messages[1].Content), &payload)
		a := model.AnalysisResult{Relevant: true, IdentityConfidence: 0.94, Sentiment: "neutral", RiskCategory: "normal_mention", RiskScore: 10, Summary: "Synthetic fixture: public mention", Reasons: []string{"Demonstration output, not a real AI judgment"}}
		text := strings.ToLower(payload.Text)
		if strings.Contains(text, "scam") {
			a.RiskCategory = "possible_scam"
			a.RiskScore = 92
			a.Sentiment = "negative"
			a.SentimentScore = -0.8
			a.ViolationCandidate = true
			a.RequiresReview = true
			a.Summary = "Synthetic fixture: possible impersonation scam requiring human review"
		} else if strings.Contains(text, "criticism") {
			a.RiskCategory = "criticism"
			a.Sentiment = "negative"
			a.SentimentScore = -0.5
			a.Summary = "Synthetic fixture: legitimate policy criticism"
		} else if strings.Contains(text, "award") {
			a.Sentiment = "positive"
			a.SentimentScore = 0.8
		}
		b, _ := json.Marshal(a)
		var fields map[string]any
		_ = json.Unmarshal(b, &fields)
		delete(fields, "model_name")
		delete(fields, "analyzed_at")
		b, _ = json.Marshal(fields)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": ollama.Message{Role: "assistant", Content: string(b)}, "done": true})
	}))
}
