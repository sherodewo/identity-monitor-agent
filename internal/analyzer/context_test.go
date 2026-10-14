package analyzer

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/model"
	"testing"
)

func TestContextSchemaAndValidation(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal(ContextSchema, &schema))
	properties := schema["properties"].(map[string]any)
	require.NotContains(t, properties, "sentiment")
	require.NotContains(t, properties, "sentiment_score")
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(valid), &fields))
	delete(fields, "sentiment")
	delete(fields, "sentiment_score")
	b, err := json.Marshal(fields)
	require.NoError(t, err)
	result, err := ValidateContext(string(b))
	require.NoError(t, err)
	require.Equal(t, "criticism", result.RiskCategory)
	_, err = ValidateContext(valid)
	require.Error(t, err)
	_, err = ValidateContext(string(b) + ` {}`)
	require.Error(t, err)
	local := Local{Client: &mock{responses: []string{string(b)}}, Threshold: 70, ContextOnly: true}
	result, err = local.Analyze(context.Background(), model.Subject{}, model.RawMention{})
	require.NoError(t, err)
	require.Equal(t, "unknown", result.Sentiment)
}
