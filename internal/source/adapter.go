package source

import (
	"context"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"identity-monitor-agent/internal/browser"
	"identity-monitor-agent/internal/config"
	"identity-monitor-agent/internal/extractor"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/model"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SourceAdapter interface {
	Name() string
	Search(context.Context, model.SearchTask) ([]model.RawMention, error)
	CheckSession(context.Context) model.SessionStatus
}
type Fallback interface {
	Recover(context.Context, browser.SessionAPI, model.SearchTask, guardrail.Policy) ([]model.RawMention, error)
}
type Adapter struct {
	Config     config.Source
	Browser    *browser.Manager
	Fallback   Fallback
	Guardrails config.Guardrails
	mu         sync.Mutex
	status     model.SessionStatus
}

func (a *Adapter) Name() string { return a.Config.Name }
func (a *Adapter) CheckSession(_ context.Context) model.SessionStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status == "" {
		return model.SessionUnknown
	}
	return a.status
}
func (a *Adapter) Search(ctx context.Context, task model.SearchTask) (out []model.RawMention, err error) {
	policy := guardrail.Policy{Domains: a.Config.AllowedDomains, MaxSteps: a.Guardrails.MaxAgentSteps, MaxScroll: a.Browser.Config.MaxScrollPerQuery, MaxNavigation: a.Guardrails.MaxNavigationPerTask}
	session, e := a.Browser.Open(ctx, a.Name(), policy, false)
	if e != nil {
		return nil, e
	}
	defer session.Close()
	defer func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if err != nil {
			a.status = model.SessionUnknown
			if strings.Contains(err.Error(), "HUMAN_ACTION_REQUIRED") {
				a.status = model.SessionHuman
			}
			_, _ = session.Screenshot()
		} else {
			a.status = model.SessionReady
		}
	}()
	if e = session.Navigate(fmt.Sprintf(a.Config.SearchURL, url.QueryEscape(task.Query))); e != nil {
		return nil, e
	}
	if e = wait(ctx, a.Browser.Config.DelayMilliseconds); e != nil {
		return nil, e
	}
	out, e = collectPages(ctx, session, a.Name(), task.MaxResults, a.Browser.Config.MaxScrollPerQuery, a.Browser.Config.DelayMilliseconds)
	if e != nil && len(out) == 0 && a.Fallback != nil && !errors.Is(e, guardrail.ErrHuman) {
		out, e = a.Fallback.Recover(ctx, session, task, policy)
	}
	if e != nil {
		return out, e
	}
	// Keep the text collected from search cards. A later detail-page failure must
	// not discard the batch, or attach one post's screenshot to other posts.

	return out, nil
}
func wait(ctx context.Context, ms int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return nil
	}
}

type selectors struct{ Card, Link, Author, Handle, Date, Text string }

var sourceSelectors = map[string]selectors{
	"generic_web_search":    {Card: "div.MjjYud, div.g, article.search-result", Link: "a:has(h3), a.result-link", Author: ".author", Handle: ".handle", Date: "time", Text: ".VwiC3b, .snippet"},
	"youtube_public_search": {Card: "ytd-video-renderer, article.video-result", Link: "a#video-title, a.result-link", Author: "ytd-channel-name, .author", Handle: ".handle", Date: "time", Text: "#description-text, .snippet"},
	"threads_public_search": {Card: `article, div[data-pressable-container="true"], article.thread-result`, Link: `a[href*="/post/"], a.result-link`, Author: `a[href^="/@"], .author`, Handle: `a[href^="/@"], .handle`, Date: "time", Text: `div[dir="auto"], .content`},
}

func Extract(source, base, html string, limit int) ([]model.RawMention, error) {
	sel, ok := sourceSelectors[source]
	if !ok {
		return nil, fmt.Errorf("unsupported source")
	}
	doc, e := goquery.NewDocumentFromReader(strings.NewReader(html))
	if e != nil {
		return nil, e
	}
	doc.Find("script,style,noscript").Remove()
	if e = guardrail.Detect(base, html, doc.Text()); e != nil {
		return nil, e
	}
	if source == "threads_public_search" {
		return extractThreads(doc, base, limit)
	}
	out := []model.RawMention{}
	seen := map[string]bool{}
	baseURL, e := url.Parse(base)
	if e != nil {
		return nil, e
	}
	doc.Find(sel.Card).Each(func(_ int, card *goquery.Selection) {
		if len(out) >= limit {
			return
		}
		link := card.Find(sel.Link).First()
		href, ok := link.Attr("href")
		if !ok {
			return
		}
		u, e := url.Parse(href)
		if e != nil {
			return
		}
		u = baseURL.ResolveReference(u)
		if u.Path == "/url" {
			target := u.Query().Get("q")
			if target == "" {
				target = u.Query().Get("url")
			}
			if parsed, e := url.Parse(target); e == nil && parsed.Host != "" {
				u = parsed
			}
		}
		canonical, e := extractor.URL(u.String())
		if e != nil || seen[canonical] {
			return
		}
		title := extractor.Text(link.Text())
		if title == "" && source != "threads_public_search" {
			return
		}
		m := model.RawMention{Source: source, SourceURL: canonical, AuthorName: extractor.Text(card.Find(sel.Author).First().Text()), AuthorHandle: card.Find(sel.Handle).First().Text(), ContentText: title + " " + extractor.Text(card.Find(sel.Text).First().Text()), CollectedAt: time.Now().UTC(), Engagement: map[string]int64{}, MediaURLs: []string{}, RawMetadata: map[string]any{"search_page": base, "title": title}}
		m.RawMetadata["text_origin"] = "search_excerpt"
		if source == "threads_public_search" {
			var paragraphs []string
			card.Find(sel.Text).Each(func(_ int, block *goquery.Selection) {
				if block.Find(sel.Text).Length() > 0 || block.Closest("a, button, time").Length() > 0 {
					return
				}
				// Exclude nested quoted/recommended posts with their own permalink.
				owner := block.Closest(sel.Card)
				if owner.Length() > 0 && owner.Get(0) != card.Get(0) {
					return
				}
				text := extractor.Text(block.Text())
				if text != "" && text != m.AuthorName {
					paragraphs = append(paragraphs, text)
				}
			})
			m.ContentText = strings.Join(paragraphs, "\n\n")
			m.RawMetadata["text_origin"] = "post_visible_text"
			if m.ContentText == "" {
				return
			}
		}
		if v, ok := card.Find(sel.Date).First().Attr("datetime"); ok {
			if t, e := time.Parse(time.RFC3339, v); e == nil {
				m.PublishedAt = &t
			}
		}
		for _, key := range []string{"views", "likes", "comments"} {
			if raw, ok := card.Attr("data-" + key); ok {
				if n, e := strconv.ParseInt(raw, 10, 64); e == nil && n >= 0 {
					m.Engagement[key] = n
				}
			}
		}
		card.Find("img").Each(func(_ int, img *goquery.Selection) {
			raw, ok := img.Attr("src")
			if ok {
				if u, e := url.Parse(raw); e == nil {
					v := baseURL.ResolveReference(u)
					if v.Scheme == "https" {
						m.MediaURLs = append(m.MediaURLs, v.String())
					}
				}
			}
		})
		seen[canonical] = true
		out = append(out, m)
	})
	if len(out) == 0 {
		t := strings.ToLower(doc.Text())
		if strings.Contains(t, "did not match any documents") || strings.Contains(t, "no results found") || strings.Contains(t, "tidak ada hasil") {
			return out, nil
		}
		return nil, guardrail.ErrLayout
	}
	return out, nil
}

// Accumulate results across virtualized pages instead of replacing earlier posts.
func collectPages(ctx context.Context, session browser.SessionAPI, source string, limit, scrolls, delay int) ([]model.RawMention, error) {
	out := []model.RawMention{}
	seen := map[string]int{}
	var lastErr error
	for step := 0; step <= scrolls; step++ {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		page, err := session.Observe()
		if err != nil {
			return out, err
		}
		posts, err := Extract(source, page.URL, page.HTML, limit)
		if errors.Is(err, guardrail.ErrHuman) {
			return out, err
		}
		lastErr = err
		for _, post := range posts {
			if index, ok := seen[post.SourceURL]; ok {
				if len(post.ContentText) > len(out[index].ContentText) {
					out[index] = post
				}
			} else if limit == 0 || len(out) < limit {
				seen[post.SourceURL] = len(out)
				out = append(out, post)
			}
		}
		slog.Info("search scroll progress", "source", source, "scroll", step, "unique_results", len(out), "max_results", limit)
		if (limit > 0 && len(out) >= limit) || step == scrolls {
			break
		}
		if err = session.Scroll(800); err != nil {
			return out, err
		}
		if err = wait(ctx, delay); err != nil {
			return out, err
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	return out, lastErr
}
