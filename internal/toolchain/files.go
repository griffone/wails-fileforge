package toolchain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// statFile wraps os.Stat so callers can treat "missing" uniformly.
func statFile(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// fileExists reports whether path is an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// fileWithContent reports whether path is a non-empty regular file. A zero-byte
// download left by an interrupted process must be repaired, not trusted.
func fileWithContent(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// fileHasExactSize reports whether path is a regular file with exactly size
// bytes. Pinned model files use it as a cheap readiness check.
func fileHasExactSize(path string, size int64) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() == size
}

// copyFileContent copies sourcePath into destPath through a temp file, so an
// interrupted copy never leaves a truncated asset behind.
func copyFileContent(sourcePath, destPath string) error {
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", sourcePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(destPath), err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(destPath), ".tessdata-*")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", filepath.Dir(destPath), err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(payload); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write %s: %w", tempPath, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tempPath, err)
	}
	if err := os.Chmod(tempPath, 0o644); err != nil {
		return fmt.Errorf("prepare %s: %w", tempPath, err)
	}

	return replaceFile(tempPath, destPath)
}

// directoryExists reports whether path is an existing directory.
func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// replaceFile renames tempPath into destPath, replacing an existing file.
// Windows refuses a rename over an existing file, so the destination is removed
// when the first rename fails.
func replaceFile(tempPath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(destPath), err)
	}

	if err := os.Rename(tempPath, destPath); err == nil {
		return nil
	}
	if err := os.Remove(destPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace %s: %w", destPath, err)
	}

	return os.Rename(tempPath, destPath)
}

// moveEntry renames one extracted entry into its final location. Extraction and
// installation both happen inside the toolchain folder, so a rename never
// crosses filesystems.
func moveEntry(sourcePath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(destPath), err)
	}

	return os.Rename(sourcePath, destPath)
}

// withEnvironment returns base with the additions applied, replacing any
// existing value for the same key. A duplicate key in the child environment is
// ambiguous (getenv usually returns the first match), so the old entry is
// removed instead of appended.
func withEnvironment(base []string, additions map[string]string) []string {
	if len(additions) == 0 {
		return append([]string{}, base...)
	}

	keys := make([]string, 0, len(additions))
	overridden := make(map[string]struct{}, len(additions))
	for key := range additions {
		keys = append(keys, key)
		overridden[key] = struct{}{}
	}
	sort.Strings(keys)

	merged := make([]string, 0, len(base)+len(additions))
	for _, entry := range base {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, skip := overridden[key]; skip {
				continue
			}
		}

		merged = append(merged, entry)
	}
	for _, key := range keys {
		merged = append(merged, key+"="+additions[key])
	}

	return merged
}
