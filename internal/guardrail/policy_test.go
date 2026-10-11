package guardrail

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPolicy(t *testing.T) {
	p := Policy{Domains: []string{"www.youtube.com"}, MaxSteps: 20, MaxScroll: 3, MaxNavigation: 10}
	for _, url := range []string{"http://www.youtube.com", "https://www.youtube.com.evil.org", "https://www.youtube.com@evil.org", "file:///etc/passwd", "https://www.youtube.com/account", "https://www.youtube.com:444"} {
		require.Error(t, p.URL(url), url)
	}
	require.NoError(t, p.URL("https://www.youtube.com/results?search_query=test"))
	for _, raw := range []string{`{"tool":"shell","arguments":{}}`, `{"tool":"finish","arguments":{"url":"https://evil.org"}}`, `{"tool":"scroll_page","arguments":{}}`, `{"tool":"finish","arguments":{}} {}`, `{"tool":"finish","arguments":null}`} {
		_, e := Decode(raw)
		require.Error(t, e, raw)
	}
	a, e := Decode(`{"tool":"scroll_page","arguments":{"pixels":500}}`)
	require.NoError(t, e)
	require.NoError(t, p.Validate(a, 1, 0, 0))
	require.Error(t, p.Validate(a, 1, 3, 0))
	require.Error(t, p.Validate(a, 21, 0, 0))
	require.Error(t, p.Validate(Action{Tool: "click_element", Arguments: Arguments{Selector: "#report"}}, 1, 0, 0))
}
func TestChallenges(t *testing.T) {
	for _, v := range []struct{ url, html, text string }{{"https://accounts.google.com/signin", "", ""}, {"https://example.org", "<input type=\"password\">", ""}, {"https://example.org", "", "Verify you are human"}, {"https://example.org", "", "Enter your verification code"}} {
		require.ErrorIs(t, Detect(v.url, v.html, v.text), ErrHuman)
	}
	require.NoError(t, Detect("https://example.org/article", "<p>News</p>", "An article discussing CAPTCHA research"))
}
