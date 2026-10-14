package model

import "time"

type RawMention struct {
	Source         string           `json:"source"`
	SourceURL      string           `json:"source_url"`
	AuthorName     string           `json:"author_name"`
	AuthorHandle   string           `json:"author_handle"`
	ContentText    string           `json:"content_text"`
	PublishedAt    *time.Time       `json:"published_at"`
	CollectedAt    time.Time        `json:"collected_at"`
	Engagement     map[string]int64 `json:"engagement"`
	MediaURLs      []string         `json:"media_urls"`
	ScreenshotPath string           `json:"screenshot_path"`
	RawMetadata    map[string]any   `json:"raw_metadata"`
}
type Mention struct {
	Relevance      string `json:"relevance"`
	ID             int64  `json:"id"`
	SubjectID      string `json:"subject_id"`
	ContentHash    string `json:"content_hash"`
	AnalysisStatus string `json:"analysis_status"`
	RawMention
	Analysis AnalysisResult `json:"analysis"`
}
type MentionFilter struct {
	Relevance                                  string
	Execution                                  string
	Subject, Source, Date, Sentiment, Category string
	MinRisk, Limit, Offset                     int
	Review                                     bool
}
