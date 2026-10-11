package browser_test

import (
	"context"
	"github.com/chromedp/chromedp"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/browser"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/demo"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/observability"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChromeFixture(t *testing.T) {
	if os.Getenv("BROWSER_TEST") != "1" {
		t.Skip("set BROWSER_TEST=1 for local Chrome smoke test")
	}
	b, e := demo.Fixtures.ReadFile("fixtures/generic.html")
	require.NoError(t, e)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(b) }))
	defer server.Close()
	c := config.Defaults().Browser
	c.Headless = true
	c.ProfileDirectory = filepath.Join(t.TempDir(), "profiles")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	manager := browser.New(c, t.TempDir(), observability.New())
	session, e := manager.Open(ctx, "generic_web_search", guardrail.Policy{Domains: []string{"localhost"}}, true)
	require.NoError(t, e)
	defer session.Close()
	require.NoError(t, session.BootstrapNavigate(server.URL))
	path, e := session.Screenshot()
	require.NoError(t, e)
	info, e := os.Stat(filepath.Join(manager.EvidenceDir, path))
	require.NoError(t, e)
	require.Greater(t, info.Size(), int64(1000))
}
func TestDashboardChrome(t *testing.T) {
	url := os.Getenv("DASHBOARD_TEST_URL")
	if url == "" {
		t.Skip("set DASHBOARD_TEST_URL for running-dashboard Chrome smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	alloc, stop := chromedp.NewExecAllocator(ctx, append(chromedp.DefaultExecAllocatorOptions[:], chromedp.WindowSize(1440, 1100))...)
	defer stop()
	tab, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	var count int
	var mode string
	require.NoError(t, chromedp.Run(tab, chromedp.Navigate(url), chromedp.WaitVisible("#stats .stat"), chromedp.Text("#mode", &mode), chromedp.Evaluate(`document.querySelectorAll('#stats .stat').length`, &count)))
	require.Equal(t, 4, count)
	require.Contains(t, mode, "DEMO")
	require.NoError(t, chromedp.Run(tab, chromedp.Click(`nav [data-tab="results"]`), chromedp.WaitVisible(".post-card"), chromedp.Click(".post-card [data-mention]"), chromedp.WaitVisible("dialog[open] .full-scraped-text")))
	var text string
	require.NoError(t, chromedp.Run(tab, chromedp.Text(".full-scraped-text", &text)))
	require.NotEmpty(t, text)
	require.NoError(t, chromedp.Run(tab, chromedp.Click("#close-detail")))
	for _, selector := range []string{`nav [data-tab="results"]`, `nav [data-tab="mentions"]`, `nav [data-tab="review"]`, `nav [data-tab="cases"]`, `nav [data-tab="runs"]`} {
		require.NoError(t, chromedp.Run(tab, chromedp.Click(selector), chromedp.Sleep(250*time.Millisecond)))
	}
	require.NoError(t, chromedp.Run(tab, chromedp.Click(`nav [data-tab="overview"]`)))
	require.NoError(t, chromedp.Run(tab, chromedp.Click(`nav [data-tab="runs"]`), chromedp.WaitVisible("#query-tags .query-tag"), chromedp.SetValue("#run-query", "Query tambahan uji"), chromedp.Click("#query-add")))
	var tags int
	require.NoError(t, chromedp.Run(tab, chromedp.Evaluate(`document.querySelectorAll('#query-tags .query-tag').length`, &tags)))
	require.Equal(t, 2, tags)
	require.NoError(t, chromedp.Run(tab, chromedp.Click("#run-submit"), chromedp.Poll(`document.querySelector('#queue-status').textContent.includes('2/2 pencarian selesai')`, nil, chromedp.WithPollingTimeout(25*time.Second))))
	require.NoError(t, chromedp.Run(tab, chromedp.Click(`nav [data-tab="overview"]`)))
	var screenshot []byte
	require.NoError(t, chromedp.Run(tab, chromedp.FullScreenshot(&screenshot, 85)))
	path := os.Getenv("DASHBOARD_SCREENSHOT")
	if path != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
		require.NoError(t, os.WriteFile(path, screenshot, 0600))
	}
}
