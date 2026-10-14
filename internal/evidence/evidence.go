package evidence

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"identity-monitor-agent/internal/securefs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Save stores screenshot data under a random, non-guessable filename and
// returns the filename relative to the evidence directory.
func Save(directory string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty screenshot")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", fmt.Errorf("create evidence directory: %w", err)
	}
	if err := securefs.Restrict(directory, true); err != nil {
		return "", fmt.Errorf("restrict evidence directory: %w", err)
	}

	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate evidence name: %w", err)
	}
	name := hex.EncodeToString(random) + ".png"
	path := filepath.Join(directory, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("create evidence: %w", err)
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write evidence: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close evidence: %w", closeErr)
	}
	if err = securefs.Restrict(path, false); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("restrict evidence: %w", err)
	}
	return name, nil
}

// Retain removes expired screenshot files unless a case still references them.
func Retain(directory string, days int, protected map[string]bool) error {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && validName(name) && !protected[name] {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if info.ModTime().Before(cutoff) {
				if err = os.Remove(filepath.Join(directory, name)); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
	}
	return nil
}

func validName(name string) bool {
	if len(name) != 36 || !strings.HasSuffix(name, ".png") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimSuffix(name, ".png"))
	return err == nil
}
