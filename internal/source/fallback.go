package source

import (
	"github.com/PuerkitoBio/goquery"
	"identity-monitor-agent/internal/extractor"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/model"
	"net/url"
	"strings"
	"time"
)

// VisibleLinks is a conservative alternate extractor for changed search layouts.
// Every result is marked low-confidence and enters human review.
func VisibleLinks(task model.SearchTask, base, html string) ([]model.RawMention, error) {
	doc, e := goquery.NewDocumentFromReader(strings.NewReader(html))
	if e != nil {
		return nil, e
	}
	doc.Find("script,style,noscript,nav,header,footer,form").Remove()
	if e = guardrail.Detect(base, html, doc.Text()); e != nil {
		return nil, e
	}
	root, e := url.Parse(base)
	if e != nil {
		return nil, e
	}
	out := []model.RawMention{}
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if len(out) >= task.MaxResults {
			return
		}
		text := extractor.Text(a.Text())
		if len(text) < 15 || len(text) > 1000 {
			return
		}
		raw, _ := a.Attr("href")
		u, e := url.Parse(raw)
		if e != nil {
			return
		}
		u = root.ResolveReference(u)
		if task.Source == "youtube_public_search" && u.Path != "/watch" {
			return
		}
		if task.Source == "threads_public_search" && !strings.Contains(u.Path, "/post/") {
			return
		}
		if task.Source == "generic_web_search" && u.Hostname() == root.Hostname() {
			return
		}
		link, e := extractor.URL(u.String())
		if e != nil || seen[link] {
			return
		}
		seen[link] = true
		out = append(out, model.RawMention{Source: task.Source, SourceURL: link, ContentText: text, CollectedAt: time.Now().UTC(), Engagement: map[string]int64{}, MediaURLs: []string{}, RawMetadata: map[string]any{"extraction_confidence": "low", "search_page": base}})
	})
	if len(out) == 0 {
		return nil, guardrail.ErrHuman
	}
	return out, nil
}
