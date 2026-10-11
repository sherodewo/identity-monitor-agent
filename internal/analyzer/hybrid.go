package analyzer

import (
	"context"

	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/sentiment"
)

type Hybrid struct {
	Base      Analyzer
	Sentiment sentiment.Classifier
}

func (h *Hybrid) Analyze(ctx context.Context, subject model.Subject, mention model.RawMention) (model.AnalysisResult, error) {
	analysis, baseErr := h.Base.Analyze(ctx, subject, mention)
	result, sentimentErr := h.Sentiment.Classify(ctx, mention.ContentText)
	if sentimentErr != nil {
		analysis.Sentiment = "unknown"
		analysis.SentimentScore = 0
		analysis.SentimentConfidence = 0
		analysis.SentimentModel = "indobert:error"
		analysis.RequiresReview = true
		analysis.Reasons = append(analysis.Reasons, "IndoBERT sentiment analysis failed")
		if baseErr != nil {
			return analysis, baseErr
		}
		return analysis, nil
	}
	analysis.Sentiment = result.Label
	analysis.SentimentScore = result.Score
	analysis.SentimentConfidence = result.Confidence
	analysis.SentimentModel = result.Model
	if result.Label == "unknown" {
		analysis.RequiresReview = true
		analysis.Reasons = append(analysis.Reasons, "IndoBERT sentiment confidence below threshold")
	}
	return analysis, baseErr
}
