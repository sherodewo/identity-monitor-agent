package analyzer

import (
	"encoding/json"
	"fmt"
	"identity-monitor-agent/internal/model"
)

var ContextSchema = func() json.RawMessage {
	var schema struct {
		Type                 string                     `json:"type"`
		AdditionalProperties bool                       `json:"additionalProperties"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
	}
	_ = json.Unmarshal(Schema, &schema)
	delete(schema.Properties, "sentiment")
	delete(schema.Properties, "sentiment_score")
	required := []string{}
	for _, key := range schema.Required {
		if key != "sentiment" && key != "sentiment_score" {
			required = append(required, key)
		}
	}
	schema.Required = required
	b, _ := json.Marshal(schema)
	return b
}()

func ValidateContext(raw string) (model.AnalysisResult, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return model.AnalysisResult{}, err
	}
	if len(fields) != 8 || fields["sentiment"] != nil || fields["sentiment_score"] != nil {
		return model.AnalysisResult{}, fmt.Errorf("context analysis must contain exactly eight fields without sentiment")
	}
	fields["sentiment"] = json.RawMessage(`"unknown"`)
	fields["sentiment_score"] = json.RawMessage(`0`)
	b, err := json.Marshal(fields)
	if err != nil {
		return model.AnalysisResult{}, err
	}
	return Validate(string(b))
}
