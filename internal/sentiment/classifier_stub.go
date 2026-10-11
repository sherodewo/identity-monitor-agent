//go:build !cgo

package sentiment

import (
	"fmt"

	"identity-monitor-agent/internal/config"
)

func New(config.Sentiment) (Classifier, error) {
	return nil, fmt.Errorf("IndoBERT requires a CGO-enabled build")
}
