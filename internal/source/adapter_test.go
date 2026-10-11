package source_test

import (
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/demo"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/source"
	"testing"
)

func TestFixtures(t *testing.T) {
	for _, v := range []struct {
		name, adapter string
		n             int
	}{{"generic", "generic_web_search", 3}, {"youtube", "youtube_public_search", 1}, {"threads", "threads_public_search", 1}} {
		b, e := demo.Fixtures.ReadFile("fixtures/" + v.name + ".html")
		require.NoError(t, e)
		m, e := source.Extract(v.adapter, "https://example.org", string(b), 10)
		require.NoError(t, e)
		require.Len(t, m, v.n)
		require.NotEmpty(t, m[0].ContentText)
	}
	b, e := demo.Fixtures.ReadFile("fixtures/captcha.html")
	require.NoError(t, e)
	_, e = source.Extract("generic_web_search", "https://example.org", string(b), 10)
	require.ErrorIs(t, e, guardrail.ErrHuman)
	m, e := source.Extract("generic_web_search", "https://example.org", "<p>No results found</p>", 10)
	require.NoError(t, e)
	require.Empty(t, m)
	_, e = source.Extract("generic_web_search", "https://example.org", "<p>New unexplained layout</p>", 10)
	require.ErrorIs(t, e, guardrail.ErrLayout)
}

func TestThreadsCollectsAllParagraphsWithoutLinkText(t *testing.T) {
	html := `<article><a href="/@writer">writer</a><a href="/@writer/post/123"><time datetime="2026-10-06T01:00:00Z"></time></a><div dir="auto"><div dir="auto">Paragraf pertama.</div><div dir="auto">Ini ucapan selanjutnya yang sebelumnya hilang.</div></div><button><div dir="auto">Like</div></button></article>`
	posts, err := source.Extract("threads_public_search", "https://www.threads.com/search", html, 10)
	require.NoError(t, err)
	require.Len(t, posts, 1)
	require.Equal(t, "Paragraf pertama.\n\nIni ucapan selanjutnya yang sebelumnya hilang.", posts[0].ContentText)
	require.NotNil(t, posts[0].PublishedAt)
}

func TestThreadsUnmarkedCardsAndNoExactQueryRequirement(t *testing.T) {
	html := `<main><div class="feed"><div class="random"><div><a href="/@a">Alice</a><a href="/@a/post/1?tracking=x"><time>2h</time></a></div><div><span>Saya kecewa dengan kebijakan ini.</span><br><span>Semoga ada perbaikan.</span></div><button>Like</button></div><div class="other"><a href="/@b/post/2">3h</a><span>Programnya sangat membantu warga.</span></div></div></main>`
	posts, err := source.Extract("threads_public_search", "https://www.threads.com/search?q=Prabowo", html, 0)
	require.NoError(t, err)
	require.Len(t, posts, 2)
	require.Contains(t, posts[0].ContentText, "Semoga ada perbaikan.")
	require.NotContains(t, posts[0].ContentText, "Like")
	require.NotContains(t, posts[0].ContentText, "Programnya")
	require.Equal(t, "Programnya sangat membantu warga.", posts[1].ContentText)
	require.Equal(t, "https://www.threads.com/@a/post/1", posts[0].SourceURL)
}
