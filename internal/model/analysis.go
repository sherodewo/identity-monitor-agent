package model

import "time"

type AnalysisResult struct {
	Relevant            bool      `json:"relevant"`
	IdentityConfidence  float64   `json:"identity_confidence"`
	Sentiment           string    `json:"sentiment"`
	SentimentScore      float64   `json:"sentiment_score"`
	SentimentConfidence float64   `json:"sentiment_confidence,omitempty"`
	SentimentModel      string    `json:"sentiment_model,omitempty"`
	RiskCategory        string    `json:"risk_category"`
	RiskScore           int       `json:"risk_score"`
	ViolationCandidate  bool      `json:"violation_candidate"`
	Summary             string    `json:"summary"`
	Reasons             []string  `json:"reasons"`
	RequiresReview      bool      `json:"requires_review"`
	ModelName           string    `json:"model_name"`
	AnalyzedAt          time.Time `json:"analyzed_at"`
}
