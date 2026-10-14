package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/observability"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ChatClient interface {
	Chat(context.Context, []Message, any) (string, error)
	Model() string
	Health(context.Context) error
}
type Client struct {
	Config  config.Ollama
	HTTP    *http.Client
	Metrics *observability.Metrics
}

func New(c config.Ollama, m *observability.Metrics) *Client {
	return &Client{Config: c, HTTP: &http.Client{Timeout: time.Duration(c.TimeoutSeconds) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, Metrics: m}
}
func (c *Client) Model() string { return c.Config.TextModel }
func (c *Client) Health(ctx context.Context) error {
	if c.Config.TextModel == "" {
		return fmt.Errorf("set OLLAMA_TEXT_MODEL or ollama.text_model; download it with ollama pull <model>")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Config.BaseURL, "/")+"/api/tags", nil)
	if e != nil {
		return e
	}
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return fmt.Errorf("local Ollama unavailable: %w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("ollama health HTTP %d", resp.StatusCode)
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tags); e != nil {
		return e
	}
	for _, want := range []string{c.Config.TextModel, c.Config.VisionModel} {
		if want == "" {
			continue
		}
		found := false
		for _, m := range tags.Models {
			if m.Name == want || m.Name == want+":latest" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("ollama model %q is missing; run ollama pull %s", want, want)
		}
	}
	return nil
}
func (c *Client) Chat(ctx context.Context, messages []Message, schema any) (string, error) {
	start := time.Now()
	defer func() {
		if c.Metrics != nil {
			c.Metrics.OllamaDuration.Observe(time.Since(start).Seconds())
		}
	}()
	body, e := json.Marshal(map[string]any{"model": c.Config.TextModel, "messages": messages, "format": schema, "stream": false, "options": map[string]any{"temperature": 0, "num_ctx": 4096, "num_predict": 500}})
	if e != nil {
		return "", e
	}
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		c.Metrics.Inc("ollama_requests_total")
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.Config.BaseURL, "/")+"/api/chat", bytes.NewReader(body))
		if e != nil {
			return "", e
		}
		req.Header.Set("Content-Type", "application/json")
		resp, e := c.HTTP.Do(req)
		if e != nil {
			last = fmt.Errorf("local inference request failed: %w", e)
		} else {
			b, re := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			_ = resp.Body.Close()
			if re != nil {
				last = re
			} else if resp.StatusCode == 200 {
				var out struct {
					Message Message `json:"message"`
					Done    bool    `json:"done"`
					Error   string  `json:"error"`
				}
				if e = json.Unmarshal(b, &out); e != nil {
					return "", fmt.Errorf("invalid Ollama envelope")
				}
				if !out.Done || out.Error != "" || out.Message.Content == "" {
					return "", fmt.Errorf("incomplete Ollama response")
				}
				return out.Message.Content, nil
			} else {
				last = fmt.Errorf("ollama HTTP %d", resp.StatusCode)
				if resp.StatusCode < 500 {
					return "", last
				}
			}
		}
		if attempt == 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
	return "", last
}
