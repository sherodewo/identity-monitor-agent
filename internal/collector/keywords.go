package collector

import (
	"identity-monitor-agent/internal/model"
	"strings"
)

func Keywords(s model.Subject) []string {
	out := []string{}
	seen := map[string]bool{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	quote := func(v string) string { return `"` + strings.ReplaceAll(strings.TrimSpace(v), `"`, "") + `"` }
	for _, name := range append(append([]string{s.FullName, s.PopularName}, s.Aliases...), s.SpellingVariants...) {
		if strings.TrimSpace(name) != "" {
			add(quote(name))
		}
	}
	for _, v := range []string{s.Position, s.Institution} {
		if v != "" {
			add(quote(s.FullName) + " " + quote(v))
		}
	}
	for _, v := range s.RiskKeywords {
		if strings.TrimSpace(v) != "" {
			add(quote(s.FullName) + " " + strings.TrimSpace(v))
		}
	}
	for _, v := range s.OfficialHandles {
		add(quote(v))
	}
	return out
}
