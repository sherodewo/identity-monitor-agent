package config

import (
	"bufio"
	"fmt"
	"gopkg.in/yaml.v3"
	"identity-monitor-agent/internal/model"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
	_ "time/tzdata"
)

// LoadEnvFile loads simple KEY=VALUE entries without overriding environment
// variables that are already set by the operator or process manager.
func LoadEnvFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return fmt.Errorf("invalid .env entry")
		}
		for i, r := range key {
			letter := r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
			digit := i > 0 && r >= '0' && r <= '9'
			if !letter && !digit {
				return fmt.Errorf("invalid .env key %q", key)
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type App struct {
	Timezone          string `yaml:"timezone"`
	HTTPAddress       string `yaml:"http_address"`
	DataDirectory     string `yaml:"data_directory"`
	EvidenceDirectory string `yaml:"evidence_directory"`
	ReportDirectory   string `yaml:"report_directory"`
	APIToken          string `yaml:"api_token"`
}
type Scheduler struct {
	Enabled           bool   `yaml:"enabled"`
	StartTime         string `yaml:"start_time"`
	StopTime          string `yaml:"stop_time"`
	DailyReportTime   string `yaml:"daily_report_time"`
	MaxConcurrentJobs int    `yaml:"max_concurrent_jobs"`
	IntervalMinutes   int    `yaml:"interval_minutes"`
	JobTimeoutSeconds int    `yaml:"job_timeout_seconds"`
}
type Browser struct {
	ExecutablePath     string `yaml:"executable_path"`
	Headless           bool   `yaml:"headless"`
	ProfileDirectory   string `yaml:"profile_directory"`
	PageTimeoutSeconds int    `yaml:"page_timeout_seconds"`
	MaxScrollPerQuery  int    `yaml:"max_scroll_per_query"`
	DelayMilliseconds  int    `yaml:"delay_milliseconds"`
}
type Ollama struct {
	BaseURL        string `yaml:"base_url"`
	TextModel      string `yaml:"text_model"`
	VisionModel    string `yaml:"vision_model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}
type Sentiment struct {
	Provider            string  `yaml:"provider"`
	ModelPath           string  `yaml:"model_path"`
	VocabularyPath      string  `yaml:"vocabulary_path"`
	RuntimeLibraryPath  string  `yaml:"runtime_library_path"`
	MaxTokens           int     `yaml:"max_tokens"`
	ConfidenceThreshold float64 `yaml:"confidence_threshold"`
}
type Guardrails struct {
	MaxAgentSteps        int  `yaml:"max_agent_steps"`
	MaxNavigationPerTask int  `yaml:"max_navigation_per_task"`
	StopOnCaptcha        bool `yaml:"stop_on_captcha"`
	StopOnLoginRequired  bool `yaml:"stop_on_login_required"`
	RequireApproval      bool `yaml:"require_approval_for_external_action"`
	AgentTimeoutSeconds  int  `yaml:"agent_timeout_seconds"`
}
type Retention struct {
	MentionDays    int `yaml:"mention_days"`
	ScreenshotDays int `yaml:"screenshot_days"`
	AuditLogDays   int `yaml:"audit_log_days"`
}
type Source struct {
	Name           string   `yaml:"name" json:"name"`
	SearchURL      string   `yaml:"search_url" json:"search_url"`
	AllowedDomains []string `yaml:"allowed_domains" json:"allowed_domains"`
	MaxResults     int      `yaml:"max_results" json:"max_results"`
	Enabled        bool     `yaml:"enabled" json:"enabled"`
}
type Config struct {
	App               App        `yaml:"app"`
	Scheduler         Scheduler  `yaml:"scheduler"`
	Browser           Browser    `yaml:"browser"`
	Ollama            Ollama     `yaml:"ollama"`
	Sentiment         Sentiment  `yaml:"sentiment"`
	Guardrails        Guardrails `yaml:"guardrails"`
	Retention         Retention  `yaml:"retention"`
	Sources           []Source   `yaml:"sources"`
	RiskThreshold     int        `yaml:"risk_threshold"`
	ImageDistance     int        `yaml:"image_distance"`
	Demo              bool       `yaml:"-"`
	AllowOutsideHours bool       `yaml:"-"`
}

func Defaults() Config {
	return Config{
		App:        App{Timezone: "Asia/Jakarta", HTTPAddress: "127.0.0.1:8080", DataDirectory: "./data", EvidenceDirectory: "./evidence", ReportDirectory: "./reports"},
		Scheduler:  Scheduler{Enabled: true, StartTime: "08:00", StopTime: "17:00", DailyReportTime: "17:00", MaxConcurrentJobs: 1, IntervalMinutes: 60, JobTimeoutSeconds: 600},
		Browser:    Browser{ProfileDirectory: "./data/browser-profiles", PageTimeoutSeconds: 45, MaxScrollPerQuery: 6, DelayMilliseconds: 3000},
		Ollama:     Ollama{BaseURL: "http://127.0.0.1:11434", TimeoutSeconds: 120},
		Sentiment:  Sentiment{Provider: "indobert", ModelPath: "./models/indobert-sentiment/model.onnx", VocabularyPath: "./models/indobert-sentiment/vocab.txt", RuntimeLibraryPath: "./runtime/onnxruntime/onnxruntime.dll", MaxTokens: 256, ConfidenceThreshold: 0.65},
		Guardrails: Guardrails{MaxAgentSteps: 20, MaxNavigationPerTask: 10, StopOnCaptcha: true, StopOnLoginRequired: true, RequireApproval: true, AgentTimeoutSeconds: 120},
		Retention:  Retention{MentionDays: 90, ScreenshotDays: 30, AuditLogDays: 365}, RiskThreshold: 70, ImageDistance: 8,
		Sources: []Source{{Name: "generic_web_search", SearchURL: "https://www.google.com/search?q=%s", AllowedDomains: []string{"www.google.com"}, MaxResults: 10, Enabled: true}, {Name: "youtube_public_search", SearchURL: "https://www.youtube.com/results?search_query=%s", AllowedDomains: []string{"www.youtube.com"}, MaxResults: 10, Enabled: true}, {Name: "threads_public_search", SearchURL: "https://www.threads.com/search?q=%s", AllowedDomains: []string{"www.threads.com"}, MaxResults: 10, Enabled: true}},
	}
}
func Load(path string) (Config, error) {
	c := Defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	d := yaml.NewDecoder(strings.NewReader(os.ExpandEnv(string(b))))
	d.KnownFields(true)
	if err = d.Decode(&c); err != nil {
		return c, err
	}
	for key, dst := range map[string]*string{"OLLAMA_BASE_URL": &c.Ollama.BaseURL, "OLLAMA_TEXT_MODEL": &c.Ollama.TextModel, "OLLAMA_VISION_MODEL": &c.Ollama.VisionModel, "LOCAL_API_TOKEN": &c.App.APIToken, "SENTIMENT_PROVIDER": &c.Sentiment.Provider, "INDOBERT_MODEL_PATH": &c.Sentiment.ModelPath, "INDOBERT_VOCAB_PATH": &c.Sentiment.VocabularyPath, "ONNXRUNTIME_LIBRARY_PATH": &c.Sentiment.RuntimeLibraryPath} {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	if v := os.Getenv("OLLAMA_TIMEOUT"); v != "" {
		dur, e := time.ParseDuration(v)
		if e != nil {
			return c, fmt.Errorf("OLLAMA_TIMEOUT: %w", e)
		}
		c.Ollama.TimeoutSeconds = int(dur.Seconds())
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if _, e := time.LoadLocation(c.App.Timezone); e != nil {
		return e
	}
	host, _, e := net.SplitHostPort(c.App.HTTPAddress)
	if e != nil {
		return e
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("dashboard must bind to a loopback IP")
	}
	u, e := url.Parse(c.Ollama.BaseURL)
	if e != nil || u.Scheme != "http" || u.User != nil {
		return fmt.Errorf("Ollama requires a local HTTP URL")
	}
	ip = net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("Ollama must be local")
	}
	for _, v := range []string{c.Scheduler.StartTime, c.Scheduler.StopTime, c.Scheduler.DailyReportTime} {
		if _, e = time.Parse("15:04", v); e != nil {
			return e
		}
	}
	if c.Scheduler.StartTime >= c.Scheduler.StopTime {
		return fmt.Errorf("start_time must precede stop_time")
	}
	if c.Scheduler.MaxConcurrentJobs < 1 || c.Scheduler.MaxConcurrentJobs > 4 || c.Scheduler.IntervalMinutes < 1 || c.Scheduler.JobTimeoutSeconds < 1 || c.Browser.PageTimeoutSeconds < 1 || c.Browser.DelayMilliseconds < 500 || c.Browser.MaxScrollPerQuery < 0 || c.Browser.MaxScrollPerQuery > 10 || c.Ollama.TimeoutSeconds < 1 {
		return fmt.Errorf("invalid concurrency, timeout, scroll or delay limit")
	}
	if c.Guardrails.MaxAgentSteps < 1 || c.Guardrails.MaxAgentSteps > 20 || c.Guardrails.MaxNavigationPerTask < 1 || c.Guardrails.MaxNavigationPerTask > 10 || c.Guardrails.AgentTimeoutSeconds < 1 || !c.Guardrails.StopOnCaptcha || !c.Guardrails.StopOnLoginRequired || !c.Guardrails.RequireApproval {
		return fmt.Errorf("unsafe guardrail configuration")
	}
	if c.Retention.MentionDays < 1 || c.Retention.ScreenshotDays < 1 || c.Retention.AuditLogDays < 1 || c.RiskThreshold < 1 || c.RiskThreshold > 100 || c.ImageDistance < 0 || c.ImageDistance > 64 {
		return fmt.Errorf("invalid retention or threshold")
	}
	if c.Sentiment.Provider != "ollama" && c.Sentiment.Provider != "indobert" {
		return fmt.Errorf("sentiment provider must be ollama or indobert")
	}
	if c.Sentiment.MaxTokens < 8 || c.Sentiment.MaxTokens > 512 || c.Sentiment.ConfidenceThreshold <= 0 || c.Sentiment.ConfidenceThreshold > 1 {
		return fmt.Errorf("invalid sentiment configuration")
	}
	seen := map[string]bool{}
	for _, s := range c.Sources {
		if seen[s.Name] || (s.Name != "generic_web_search" && s.Name != "youtube_public_search" && s.Name != "threads_public_search") {
			return fmt.Errorf("unknown or repeated source %q", s.Name)
		}
		seen[s.Name] = true
		if s.MaxResults < 0 || (s.MaxResults == 0 && s.Name != "threads_public_search") || s.MaxResults > 30 || len(s.AllowedDomains) == 0 || strings.Count(s.SearchURL, "%s") != 1 {
			return fmt.Errorf("invalid source limits or URL")
		}
	}
	return nil
}
func LoadSubjects(path string) ([]model.Subject, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return DecodeSubjects(f)
}
func DecodeSubjects(r io.Reader) ([]model.Subject, error) {
	var v struct {
		Subjects []model.Subject `yaml:"subjects"`
	}
	d := yaml.NewDecoder(r)
	d.KnownFields(true)
	if e := d.Decode(&v); e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	for _, s := range v.Subjects {
		if e := ValidateSubject(s); e != nil {
			return nil, e
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("duplicate subject id")
		}
		seen[s.ID] = true
	}
	return v.Subjects, nil
}
func ValidateSubject(s model.Subject) error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.FullName) == "" || len(s.ID) > 100 || len(s.FullName) > 200 {
		return fmt.Errorf("subject id and full_name are required (max 100/200 characters)")
	}
	return nil
}
