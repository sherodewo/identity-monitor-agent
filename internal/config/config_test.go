package config

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Setenv("OLLAMA_TEXT_MODEL", "test-local")
	c, e := Load("../../configs/config.example.yaml")
	require.NoError(t, e)
	require.Equal(t, "test-local", c.Ollama.TextModel)
	require.Equal(t, "127.0.0.1:8080", c.App.HTTPAddress)
	require.False(t, c.Browser.Headless)
	subjects, e := LoadSubjects("../../configs/subjects.example.yaml")
	require.NoError(t, e)
	require.Len(t, subjects, 1)
}
func TestInvalidConfiguration(t *testing.T) {
	for _, mutate := range []func(*Config){func(c *Config) { c.App.HTTPAddress = "0.0.0.0:8080" }, func(c *Config) { c.Ollama.BaseURL = "https://cloud.example" }, func(c *Config) { c.Guardrails.StopOnCaptcha = false }, func(c *Config) { c.Guardrails.MaxAgentSteps = 21 }, func(c *Config) { c.Scheduler.StartTime = "19:00" }, func(c *Config) { c.Scheduler.MaxConcurrentJobs = 0 }} {
		c := Defaults()
		mutate(&c)
		require.Error(t, c.Validate())
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("unknown: true"), 0600))
	_, e := Load(path)
	require.Error(t, e)
	_, e = DecodeSubjects(strings.NewReader("subjects:\n - id: a\n   full_name: Test\n - id: a\n   full_name: Other\n"))
	require.Error(t, e)
}

func TestLoadEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte("# local settings\nTEST_ENV_LOADER=from-file\nTEST_ENV_QUOTED=\"quoted value\"\n"), 0600))
	t.Setenv("TEST_ENV_LOADER", "from-process")
	t.Cleanup(func() { _ = os.Unsetenv("TEST_ENV_QUOTED") })
	require.NoError(t, LoadEnvFile(path))
	require.Equal(t, "from-process", os.Getenv("TEST_ENV_LOADER"))
	require.Equal(t, "quoted value", os.Getenv("TEST_ENV_QUOTED"))
	require.NoError(t, LoadEnvFile(filepath.Join(t.TempDir(), "missing")))
}
