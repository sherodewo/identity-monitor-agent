package guardrail

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

var ErrHuman = errors.New("HUMAN_ACTION_REQUIRED")
var ErrLayout = errors.New("extractor layout changed or low confidence")

type Action struct {
	Tool      string    `json:"tool"`
	Arguments Arguments `json:"arguments"`
}
type Arguments struct {
	URL          string `json:"url,omitempty"`
	Selector     string `json:"selector,omitempty"`
	Text         string `json:"text,omitempty"`
	Pixels       int    `json:"pixels,omitempty"`
	Milliseconds int    `json:"milliseconds,omitempty"`
}

// Each tool has a separate strict object schema; actions cannot carry arbitrary CDP/JS.
func Schema() any {
	variants := []any{}
	for _, tool := range []string{"navigate", "type_text", "click_element", "scroll_page", "extract_visible_content", "capture_screenshot", "go_back", "wait", "finish", "request_human"} {
		props := map[string]any{}
		required := []string{}
		switch tool {
		case "navigate":
			props["url"] = map[string]any{"type": "string"}
			required = []string{"url"}
		case "type_text":
			props["selector"] = map[string]any{"type": "string"}
			props["text"] = map[string]any{"type": "string"}
			required = []string{"selector", "text"}
		case "click_element":
			props["selector"] = map[string]any{"type": "string"}
			required = []string{"selector"}
		case "scroll_page":
			props["pixels"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}
			required = []string{"pixels"}
		case "wait":
			props["milliseconds"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 3000}
			required = []string{"milliseconds"}
		}
		variants = append(variants, map[string]any{"type": "object", "additionalProperties": false, "required": []string{"tool", "arguments"}, "properties": map[string]any{"tool": map[string]any{"const": tool}, "arguments": map[string]any{"type": "object", "additionalProperties": false, "properties": props, "required": required}}})
	}
	return map[string]any{"oneOf": variants}
}
func Decode(raw string) (Action, error) {
	var a Action
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&a); e != nil {
		return a, e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return a, fmt.Errorf("trailing action JSON")
	}
	var envelope map[string]json.RawMessage
	if e := json.Unmarshal([]byte(raw), &envelope); e != nil {
		return a, e
	}
	if len(envelope) != 2 || string(envelope["arguments"]) == "null" {
		return a, fmt.Errorf("tool and arguments required")
	}
	var args map[string]json.RawMessage
	if e := json.Unmarshal(envelope["arguments"], &args); e != nil {
		return a, e
	}
	expected := map[string][]string{"navigate": {"url"}, "type_text": {"selector", "text"}, "click_element": {"selector"}, "scroll_page": {"pixels"}, "extract_visible_content": {}, "capture_screenshot": {}, "go_back": {}, "wait": {"milliseconds"}, "finish": {}, "request_human": {}}
	keys, ok := expected[a.Tool]
	if !ok || len(args) != len(keys) {
		return a, fmt.Errorf("invalid action schema")
	}
	for _, k := range keys {
		if v, ok := args[k]; !ok || string(v) == "null" {
			return a, fmt.Errorf("missing argument %s", k)
		}
	}
	return a, nil
}

type Policy struct {
	Domains                            []string
	MaxSteps, MaxScroll, MaxNavigation int
}

func (p Policy) URL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return fmt.Errorf("only allowlisted HTTPS URLs permitted")
	}
	allowed := false
	for _, d := range p.Domains {
		if strings.EqualFold(u.Hostname(), d) {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("domain not allowlisted")
	}
	path := strings.ToLower(u.Path)
	for _, word := range []string{"login", "signin", "sign-in", "account", "security", "report", "upload", "logout", "delete", "subscribe", "consent", "challenge", "verify"} {
		if strings.Contains(path, word) {
			return ErrHuman
		}
	}
	return nil
}
func (p Policy) Validate(a Action, step, scroll, navigation int) error {
	if step > p.MaxSteps {
		return fmt.Errorf("step limit")
	}
	switch a.Tool {
	case "navigate":
		if navigation >= p.MaxNavigation {
			return fmt.Errorf("navigation limit")
		}
		return p.URL(a.Arguments.URL)
	case "type_text", "click_element":
		return fmt.Errorf("interactive page actions disabled in MVP: human action required")
	case "scroll_page":
		if scroll >= p.MaxScroll || a.Arguments.Pixels < 1 || a.Arguments.Pixels > 1000 {
			return fmt.Errorf("scroll limit")
		}
	case "wait":
		if a.Arguments.Milliseconds < 1 || a.Arguments.Milliseconds > 3000 {
			return fmt.Errorf("wait limit")
		}
	case "extract_visible_content", "capture_screenshot", "go_back", "finish", "request_human":
	default:
		return fmt.Errorf("tool not allowlisted")
	}
	return nil
}
func Detect(rawURL, html, text string) error {
	u, _ := url.Parse(rawURL)
	if u != nil {
		path := strings.ToLower(u.Host + u.Path)
		for _, v := range []string{"accounts.google.", "/login", "/signin", "/sign-in", "/challenge", "/verify", "/checkpoint", "consent.google"} {
			if strings.Contains(path, v) {
				return ErrHuman
			}
		}
	}
	h := strings.ToLower(html)
	for _, v := range []string{`type="password"`, `type='password'`, `autocomplete="one-time-code"`, `id="captcha"`, `class="g-recaptcha"`, `id="challenge-form"`, `src="https://www.google.com/recaptcha/`} {
		if strings.Contains(h, v) {
			return ErrHuman
		}
	}
	t := strings.ToLower(text)
	for _, v := range []string{"verify you are human", "unusual traffic from your computer", "complete the captcha", "sign in to continue", "enter your verification code", "two-factor authentication", "confirm you're not a bot", "verify it's you"} {
		if strings.Contains(t, v) {
			return ErrHuman
		}
	}
	return nil
}
