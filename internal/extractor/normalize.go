package extractor

import (
	"crypto/sha256"
	"fmt"
	"identity-monitor-agent/internal/model"
	"net/url"
	"strings"
)

func Text(s string) string { return strings.Join(strings.Fields(s), " ") }
func Hash(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(Text(s))))) }
func URL(raw string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil {
		return "", e
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid public URL")
	}
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") || k == "fbclid" || k == "gclid" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String(), nil
}
func Normalize(m model.RawMention) (model.RawMention, error) {
	var e error
	m.SourceURL, e = URL(m.SourceURL)
	lines := strings.Split(strings.ReplaceAll(m.ContentText, "\r\n", "\n"), "\n")
	for i := range lines {
		lines[i] = Text(lines[i])
	}
	m.ContentText = strings.TrimSpace(strings.Join(lines, "\n"))
	m.AuthorName = Text(m.AuthorName)
	m.AuthorHandle = strings.TrimPrefix(strings.ToLower(Text(m.AuthorHandle)), "@")
	m.Source = strings.ToLower(Text(m.Source))
	m.CollectedAt = m.CollectedAt.UTC()
	if m.PublishedAt != nil {
		v := m.PublishedAt.UTC()
		m.PublishedAt = &v
	}
	return m, e
}
