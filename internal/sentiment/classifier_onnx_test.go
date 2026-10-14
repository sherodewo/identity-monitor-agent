//go:build cgo

package sentiment

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"identity-monitor-agent/internal/config"
)

func TestONNXIndoBERT(t *testing.T) {
	root := filepath.Join("..", "..")
	c := config.Defaults().Sentiment
	c.ModelPath = filepath.Join(root, "models", "indobert-sentiment", "model.onnx")
	c.VocabularyPath = filepath.Join(root, "models", "indobert-sentiment", "vocab.txt")
	runtimeLibrary := "libonnxruntime.so"
	switch runtime.GOOS {
	case "darwin":
		runtimeLibrary = "libonnxruntime.dylib"
	case "windows":
		runtimeLibrary = "onnxruntime.dll"
	}
	c.RuntimeLibraryPath = filepath.Join(root, "runtime", "onnxruntime", runtimeLibrary)
	classifier, err := New(c)
	if err != nil {
		t.Skipf("IndoBERT assets are not installed: %v", err)
	}
	t.Cleanup(func() { require.NoError(t, classifier.Close()) })

	tests := []struct {
		text string
		want string
	}{
		{"Saya sangat senang dan puas dengan hasil kerjanya.", "positive"},
		{"Acara tersebut berlangsung hari Senin di Jakarta.", "neutral"},
		{"Pelayanannya buruk sekali dan sangat mengecewakan.", "negative"},
	}
	for _, test := range tests {
		result, err := classifier.Classify(context.Background(), test.text)
		require.NoError(t, err)
		t.Logf("%s: %s (%.4f)", test.want, result.Label, result.Confidence)
		require.Equal(t, test.want, result.Label)
		require.Greater(t, result.Confidence, c.ConfidenceThreshold)
	}
}
