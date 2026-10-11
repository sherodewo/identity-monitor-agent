package source

import (
	"github.com/PuerkitoBio/goquery"
	"identity-monitor-agent/internal/extractor"
	"identity-monitor-agent/internal/guardrail"
	"identity-monitor-agent/internal/model"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Use post permalinks as stable anchors rather than generated CSS classes.
func extractThreads(doc *goquery.Document, base string, limit int) ([]model.RawMention, error) {
	root, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	canonical := func(raw string) string {
		u, e := url.Parse(raw)
		if e != nil {
			return ""
		}
		u = root.ResolveReference(u)
		if !strings.Contains(u.Path, "/post/") {
			return ""
		}
		u.RawQuery = ""
		u.Fragment = ""
		v, e := extractor.URL(u.String())
		if e != nil {
			return ""
		}
		return v
	}
	out := []model.RawMention{}
	seen := map[string]int{}
	doc.Find(`a[href*="/post/"]`).Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		target := canonical(href)
		if target == "" {
			return
		}
		var card *goquery.Selection
		for parent := link.Parent(); parent.Length() > 0 && !parent.Is("body,html,main,[role=main]"); parent = parent.Parent() {
			other := false
			parent.Find(`a[href*="/post/"]`).Each(func(_ int, a *goquery.Selection) {
				h, _ := a.Attr("href")
				if v := canonical(h); v != "" && v != target {
					other = true
				}
			})
			if other {
				break
			}
			card = parent
			if parent.Is(`article,[role=article],[data-pressable-container="true"]`) && threadBody(parent) != "" {
				break
			}
		}
		if card == nil {
			return
		}
		text := threadBody(card)
		if text == "" {
			return
		}
		if i, ok := seen[target]; ok {
			if len(text) > len(out[i].ContentText) {
				out[i].ContentText = text
			}
			return
		}
		if limit > 0 && len(out) >= limit {
			return
		}
		m := model.RawMention{Source: "threads_public_search", SourceURL: target, ContentText: text, CollectedAt: time.Now().UTC(), Engagement: map[string]int64{}, MediaURLs: []string{}, RawMetadata: map[string]any{"search_page": base, "text_origin": "post_visible_text", "extraction_method": "permalink_container"}}
		card.Find(`a[href*="/@"]`).Each(func(_ int, a *goquery.Selection) {
			h, _ := a.Attr("href")
			u, e := url.Parse(h)
			if e != nil || strings.Contains(u.Path, "/post/") || m.AuthorHandle != "" {
				return
			}
			m.AuthorHandle = strings.TrimPrefix(u.Path, "/")
			m.AuthorName = extractor.Text(a.Text())
		})
		if m.AuthorHandle == "" {
			u, _ := url.Parse(target)
			m.AuthorHandle = strings.Split(strings.TrimPrefix(u.Path, "/"), "/")[0]
		}
		if v, ok := card.Find("time[datetime]").First().Attr("datetime"); ok {
			for _, format := range []string{time.RFC3339Nano, "2006-01-02"} {
				if t, e := time.Parse(format, v); e == nil {
					m.PublishedAt = &t
					break
				}
			}
		}
		for _, key := range []string{"likes", "comments", "views"} {
			if v, ok := card.Attr("data-" + key); ok {
				if n, e := strconv.ParseInt(v, 10, 64); e == nil && n >= 0 {
					m.Engagement[key] = n
				}
			}
		}
		card.Find("img[src],video[src]").Each(func(_ int, item *goquery.Selection) {
			v, _ := item.Attr("src")
			if u, e := url.Parse(v); e == nil {
				u = root.ResolveReference(u)
				if u.Scheme == "https" {
					m.MediaURLs = append(m.MediaURLs, u.String())
				}
			}
		})
		seen[target] = len(out)
		out = append(out, m)
	})
	if len(out) == 0 {
		text := strings.ToLower(doc.Text())
		if strings.Contains(text, "no results") || strings.Contains(text, "tidak ada hasil") {
			return out, nil
		}
		return out, guardrail.ErrLayout
	}
	return out, nil
}

func threadBody(card *goquery.Selection) string {
	clone := card.Clone()
	clone.Find(`button,[role=button],time,svg,script,style,[hidden],[aria-hidden=true]`).Remove()
	clone.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		h, _ := a.Attr("href")
		if strings.Contains(h, "/post/") || strings.Contains(h, "/@") {
			a.Remove()
		}
	})
	// Preserve all text, including unmarked spans, emoji and separate paragraphs.
	clone.Find("br").ReplaceWithHtml("\n")
	clone.Find("div,p,section").AppendHtml("\n")
	var lines []string
	for _, line := range strings.Split(clone.Text(), "\n") {
		if v := extractor.Text(line); v != "" {
			lines = append(lines, v)
		}
	}
	return strings.Join(lines, "\n\n")
}
