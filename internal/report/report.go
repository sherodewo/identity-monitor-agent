package report

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"identity-monitor-agent/internal/storage"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Daily struct {
	Date               string         `json:"date"`
	Total              int            `json:"total_mentions"`
	Sources            map[string]int `json:"mentions_per_source"`
	Sentiment          map[string]int `json:"sentiment"`
	HighRisk           int            `json:"high_risk_findings"`
	RiskCategories     map[string]int `json:"risk_categories"`
	CollectionFailures int            `json:"collection_failures"`
	HumanRequired      int            `json:"human_action_required"`
	TopTopics          []string       `json:"top_topics"`
	CaseStatus         map[string]int `json:"case_status_summary"`
	Demo               bool           `json:"demo"`
}
type Service struct {
	Store     *storage.SQLite
	Directory string
	Threshold int
	Demo      bool
}

func (s *Service) Generate(ctx context.Context, date string) (Daily, error) {
	d := Daily{Date: date, Sources: map[string]int{}, Sentiment: map[string]int{"positive": 0, "neutral": 0, "negative": 0, "unknown": 0}, RiskCategories: map[string]int{"possible_impersonation": 0, "possible_scam": 0, "possible_doxxing": 0, "possible_threat": 0}, CaseStatus: map[string]int{}, TopTopics: []string{}, Demo: s.Demo}
	if _, e := time.Parse("2006-01-02", date); e != nil {
		return d, e
	}
	mentions, e := s.Store.AllMentions(ctx, date)
	if e != nil {
		return d, e
	}
	topics := map[string]int{}
	for _, m := range mentions {
		d.Total++
		d.Sources[m.Source]++
		d.Sentiment[m.Analysis.Sentiment]++
		d.RiskCategories[m.Analysis.RiskCategory]++
		if m.Analysis.RiskScore >= s.Threshold {
			d.HighRisk++
		}
		for _, word := range strings.Fields(strings.ToLower(m.ContentText)) {
			word = strings.Trim(word, ".,!?\"():;")
			if len(word) > 5 {
				topics[word]++
			}
		}
	}
	keys := []string{}
	for k := range topics {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if topics[keys[i]] == topics[keys[j]] {
			return keys[i] < keys[j]
		}
		return topics[keys[i]] > topics[keys[j]]
	})
	if len(keys) > 10 {
		keys = keys[:10]
	}
	d.TopTopics = keys
	runs, e := s.Store.Runs(ctx)
	if e != nil {
		return d, e
	}
	for _, r := range runs {
		if r.StartedAt.In(s.Store.Location).Format("2006-01-02") != date {
			continue
		}
		if r.Status == "FAILED" {
			d.CollectionFailures++
		}
		if r.Status == "HUMAN_ACTION_REQUIRED" {
			d.HumanRequired++
		}
	}
	cases, e := s.Store.Cases(ctx)
	if e != nil {
		return d, e
	}
	for _, c := range cases {
		if c.CreatedAt.In(s.Store.Location).Format("2006-01-02") <= date {
			d.CaseStatus[c.Status]++
		}
	}
	dir := filepath.Join(s.Directory, date)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return d, e
	}
	b, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		return d, e
	}
	if e = atomicWrite(filepath.Join(dir, "report.json"), b); e != nil {
		return d, e
	}
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	if e = w.Write([]string{"id", "subject", "source", "url", "author", "content", "collected_at", "sentiment", "risk_category", "risk_score", "analysis_status"}); e != nil {
		return d, e
	}
	for _, m := range mentions {
		row := []string{strconv.FormatInt(m.ID, 10), m.SubjectID, m.Source, m.SourceURL, m.AuthorName, m.ContentText, m.CollectedAt.In(s.Store.Location).Format(time.RFC3339), m.Analysis.Sentiment, m.Analysis.RiskCategory, strconv.Itoa(m.Analysis.RiskScore), m.AnalysisStatus}
		for i := range row {
			row[i] = SafeCell(row[i])
		}
		if e = w.Write(row); e != nil {
			return d, e
		}
	}
	w.Flush()
	if e = w.Error(); e != nil {
		return d, e
	}
	return d, atomicWrite(filepath.Join(dir, "mentions.csv"), []byte(buf.String()))
}
func SafeCell(v string) string {
	trim := strings.TrimLeft(v, " \t\r\n")
	if trim != "" && strings.ContainsRune("=+-@", rune(trim[0])) {
		return "'" + v
	}
	return v
}
func atomicWrite(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".report-*")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(tmp, path); e != nil {
		return fmt.Errorf("save report: %w", e)
	}
	return nil
}
