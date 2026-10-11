package source

import (
	"context"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/browser"
	"testing"
)

type scrollingFixture struct {
	pages []string
	step  int
}

func (f *scrollingFixture) Observe() (browser.Page, error) {
	return browser.Page{URL: "https://www.threads.com/search", HTML: f.pages[f.step]}, nil
}
func (f *scrollingFixture) Scroll(int) error {
	if f.step < len(f.pages)-1 {
		f.step++
	}
	return nil
}
func (*scrollingFixture) Navigate(string) error             { return nil }
func (*scrollingFixture) Screenshot() (string, error)       { return "", nil }
func (*scrollingFixture) Back() error                       { return nil }
func (*scrollingFixture) Limit(context.Context) func() bool { return func() bool { return false } }

func TestCollectVirtualizedPages(t *testing.T) {
	first := `<article><a href="/@user/post/1">post</a><div dir="auto">Postingan pertama</div></article>`
	second := `<article><a href="/@user/post/2">post</a><div dir="auto">Postingan kedua</div></article>`
	f := &scrollingFixture{pages: []string{first, second, second}}
	posts, err := collectPages(context.Background(), f, "threads_public_search", 30, 2, 0)
	require.NoError(t, err)
	require.Len(t, posts, 2)
	require.Equal(t, "Postingan pertama", posts[0].ContentText)
	require.Equal(t, "Postingan kedua", posts[1].ContentText)
}

func TestCollectRetainsResultsWhenLastPageHasNoCards(t *testing.T) {
	f := &scrollingFixture{pages: []string{`<article><a href="/@u/post/1">post</a><div dir="auto">Teks postingan</div></article>`, `<main>Loading...</main>`}}
	posts, err := collectPages(context.Background(), f, "threads_public_search", 30, 1, 0)
	require.NoError(t, err)
	require.Len(t, posts, 1)
}
