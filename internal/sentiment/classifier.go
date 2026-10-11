package sentiment

import "context"

type Result struct {
	Label      string
	Score      float64
	Confidence float64
	Model      string
}

type Classifier interface {
	Classify(context.Context, string) (Result, error)
	Close() error
}
