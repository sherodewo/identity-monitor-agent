package analyzer

import (
	"context"
	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/model"
	"identity-monitor-agent/internal/ollama"
	"testing"
)

const valid = `{"relevant":true,"identity_confidence":0.95,"sentiment":"negative","sentiment_score":-0.5,"risk_category":"criticism","risk_score":20,"violation_candidate":true,"summary":"Policy criticism","reasons":["Public debate"],"requires_review":false}`

type mock struct {
	responses []string
	calls     int
}

func (m *mock) Model() string                { return "test-local" }
func (m *mock) Health(context.Context) error { return nil }
func (m *mock) Chat(context.Context, []ollama.Message, any) (string, error) {
	v := m.responses[min(m.calls, len(m.responses)-1)]
	m.calls++
	return v, nil
}
func TestValidationPolicy(t *testing.T) {
	a, e := Validate(valid)
	require.NoError(t, e)
	a = ApplyPolicy(a, 70)
	require.False(t, a.ViolationCandidate)
	a.IdentityConfidence = 0.1
	require.True(t, ApplyPolicy(a, 70).RequiresReview)
	for _, raw := range []string{`{}`, `null`, valid + ` {}`, `{"risk_score":999}`, `{"relevant":null}`} {
		_, e = Validate(raw)
		require.Error(t, e)
	}
}
func TestRepairLimit(t *testing.T) {
	m := &mock{responses: []string{"not json", valid}}
	l := Local{Client: m, Threshold: 70}
	a, e := l.Analyze(context.Background(), model.Subject{}, model.RawMention{})
	require.NoError(t, e)
	require.Equal(t, 2, m.calls)
	require.Equal(t, "test-local", a.ModelName)
	require.False(t, a.AnalyzedAt.IsZero())
	m = &mock{responses: []string{"broken"}}
	l.Client = m
	a, e = l.Analyze(context.Background(), model.Subject{}, model.RawMention{})
	require.Error(t, e)
	require.Equal(t, 2, m.calls)
	require.True(t, a.RequiresReview)
	require.Equal(t, "unknown", a.Sentiment)
}
