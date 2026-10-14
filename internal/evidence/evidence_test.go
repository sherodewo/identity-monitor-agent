package evidence

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSaveAndRetain(t *testing.T) {
	directory := t.TempDir()
	name, err := Save(directory, []byte("png data"))
	require.NoError(t, err)
	require.True(t, validName(name))
	path := filepath.Join(directory, name)
	require.FileExists(t, path)

	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
	require.NoError(t, Retain(directory, 1, map[string]bool{name: true}))
	require.FileExists(t, path)
	require.NoError(t, Retain(directory, 1, nil))
	require.NoFileExists(t, path)
}

func TestRetainIgnoresUnmanagedFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("keep"), 0600))
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
	require.NoError(t, Retain(directory, 1, nil))
	require.FileExists(t, path)
}
