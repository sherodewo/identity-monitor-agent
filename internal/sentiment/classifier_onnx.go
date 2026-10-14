//go:build cgo

package sentiment

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
	"identity-monitor-agent/internal/config"
)

type ONNX struct {
	session    *ort.DynamicAdvancedSession
	tokenizer  *wordPieceTokenizer
	maxTokens  int
	threshold  float64
	modelName  string
	inputNames []string
	mu         sync.Mutex
}

func New(c config.Sentiment) (Classifier, error) {
	for _, path := range []string{c.ModelPath, c.VocabularyPath, c.RuntimeLibraryPath} {
		if abs, err := filepath.Abs(path); err != nil || abs == "" {
			return nil, fmt.Errorf("invalid IndoBERT asset path %q", path)
		}
	}
	tokenizer, err := loadTokenizer(c.VocabularyPath)
	if err != nil {
		return nil, fmt.Errorf("load IndoBERT vocabulary: %w; run the IndoBERT setup script for your platform", err)
	}
	ort.SetSharedLibraryPath(c.RuntimeLibraryPath)
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("initialize ONNX Runtime: %w", err)
	}
	inputs, outputs, err := ort.GetInputOutputInfo(c.ModelPath)
	if err != nil {
		_ = ort.DestroyEnvironment()
		return nil, fmt.Errorf("inspect IndoBERT model: %w", err)
	}
	if len(outputs) != 1 {
		_ = ort.DestroyEnvironment()
		return nil, fmt.Errorf("expected one IndoBERT output, got %d", len(outputs))
	}
	inputNames := make([]string, len(inputs))
	for i := range inputs {
		inputNames[i] = inputs[i].Name
	}
	outputNames := make([]string, len(outputs))
	for i := range outputs {
		outputNames[i] = outputs[i].Name
	}
	session, err := ort.NewDynamicAdvancedSession(c.ModelPath, inputNames, outputNames, nil)
	if err != nil {
		_ = ort.DestroyEnvironment()
		return nil, fmt.Errorf("load IndoBERT model: %w", err)
	}
	return &ONNX{session: session, tokenizer: tokenizer, maxTokens: c.MaxTokens, threshold: c.ConfidenceThreshold, modelName: "mdhugol/indonesia-bert-sentiment-classification-onnx-int8", inputNames: inputNames}, nil
}

func (c *ONNX) Classify(ctx context.Context, text string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	ids, mask, types := c.tokenizer.encode(text, c.maxTokens)
	shape := ort.NewShape(1, int64(c.maxTokens))
	inputIDs, err := ort.NewTensor(shape, ids)
	if err != nil {
		return Result{}, err
	}
	defer inputIDs.Destroy()
	attention, err := ort.NewTensor(shape, mask)
	if err != nil {
		return Result{}, err
	}
	defer attention.Destroy()
	tokenTypes, err := ort.NewTensor(shape, types)
	if err != nil {
		return Result{}, err
	}
	defer tokenTypes.Destroy()

	inputs := make([]ort.Value, len(c.inputNames))
	for i, name := range c.inputNames {
		switch name {
		case "input_ids":
			inputs[i] = inputIDs
		case "attention_mask":
			inputs[i] = attention
		case "token_type_ids":
			inputs[i] = tokenTypes
		default:
			return Result{}, fmt.Errorf("unsupported IndoBERT input %q", name)
		}
	}
	outputs := []ort.Value{nil}
	c.mu.Lock()
	err = c.session.Run(inputs, outputs)
	c.mu.Unlock()
	if err != nil {
		return Result{}, fmt.Errorf("IndoBERT inference: %w", err)
	}
	defer outputs[0].Destroy()
	logits, ok := outputs[0].(*ort.Tensor[float32])
	if !ok || len(logits.GetData()) != 3 {
		return Result{}, fmt.Errorf("unexpected IndoBERT output")
	}
	probabilities := softmax(logits.GetData())
	best := 0
	for i := 1; i < len(probabilities); i++ {
		if probabilities[i] > probabilities[best] {
			best = i
		}
	}
	labels := []string{"positive", "neutral", "negative"}
	confidence := probabilities[best]
	label := labels[best]
	if confidence < c.threshold {
		label = "unknown"
	}
	score := probabilities[0] - probabilities[2]
	return Result{Label: label, Score: score, Confidence: confidence, Model: c.modelName}, nil
}

func softmax(values []float32) []float64 {
	maxValue := float64(values[0])
	for _, value := range values[1:] {
		if float64(value) > maxValue {
			maxValue = float64(value)
		}
	}
	result := make([]float64, len(values))
	var total float64
	for i, value := range values {
		result[i] = math.Exp(float64(value) - maxValue)
		total += result[i]
	}
	for i := range result {
		result[i] /= total
	}
	return result
}

func (c *ONNX) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.session.Destroy(); err != nil {
		return err
	}
	return ort.DestroyEnvironment()
}
