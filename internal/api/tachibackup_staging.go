package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/makinuki/makidoku/internal/identity"
)

// stagedBackupTTL bounds how long a validated upload is kept before the sweep
// removes it.
const stagedBackupTTL = time.Hour

// dataDir returns the directory the daemon owns for local artifacts, falling
// back to a relative path when the engine is not attached.
func (s *Server) dataDir() string {
	if s.dataDirOverride != "" {
		return s.dataDirOverride
	}
	if s.engine != nil {
		if dir := s.engine.DataDir(); dir != "" {
			return dir
		}
	}
	return "data"
}

// stageTachibackupUpload stores a validated upload under the data directory
// and returns the token that addresses it. A sweep drops expired uploads so a
// validation that is never followed by an import does not accumulate files.
func stageTachibackupUpload(dataDir string, data []byte) (string, error) {
	dir := filepath.Join(dataDir, "imports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create import staging directory: %w", err)
	}
	sweepStagedBackups(dir, time.Now())
	token, err := identity.New()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, token+".tachibk"), data, 0o600); err != nil {
		return "", fmt.Errorf("stage upload: %w", err)
	}
	return token, nil
}

// readStagedTachibackup reads and removes one staged upload. The token is
// restricted to a single path segment so it cannot escape the staging
// directory.
func readStagedTachibackup(dataDir, token string) ([]byte, error) {
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, `/\`) || strings.Contains(token, "..") {
		return nil, errors.New("upload id is not valid")
	}
	path := filepath.Join(dataDir, "imports", token+".tachibk")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("the staged upload has expired; validate the file again")
	}
	_ = os.Remove(path)
	return data, nil
}

// sweepStagedBackups removes staged uploads older than the time to live.
func sweepStagedBackups(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tachibk") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > stagedBackupTTL {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}
