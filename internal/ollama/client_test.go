package ollama_test

import (
	"context"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/ollama"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthMissingModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"available:latest"}]}`))
	}))
	defer server.Close()
	c := ollama.New(config.Ollama{BaseURL: server.URL, TextModel: "missing", TimeoutSeconds: 1}, nil)
	require.ErrorContains(t, c.Health(context.Background()), "ollama pull missing")
	c.Config.TextModel = "available"
	require.NoError(t, c.Health(context.Background()))
}
func TestBoundedRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) }))
	defer server.Close()
	c := ollama.New(config.Ollama{BaseURL: server.URL, TextModel: "test", TimeoutSeconds: 1}, nil)
	_, e := c.Chat(context.Background(), nil, "json")
	require.Error(t, e)
	require.Equal(t, 2, calls)
}
