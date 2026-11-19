// Package fsx provides filesystem primitives shared by the backend: atomic
// writes, cross-platform replacement, unique path resolution, writability
// probes, and path identity helpers.
package fsx

import (
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	defaultDirMode = 0o755

	// maxUniqueAttempts bounds suffix probing so an unreadable directory can
	// never turn name resolution into an infinite loop.
	maxUniqueAttempts = 1000
)

// WriteAtomic writes data to path atomically. It creates the parent directory,
// writes a temporary file in the same directory, applies mode, syncs, closes,
// and renames the temporary file over path. The temporary file is removed when
// any step fails.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	return WriteAtomicWithPattern(path, data, mode, "")
}

// WriteAtomicWithPattern is WriteAtomic with a custom temporary file pattern.
// The pattern follows os.CreateTemp semantics: the last "*" is replaced with a
// random string. An empty pattern selects the default pattern derived from the
// destination name.
func WriteAtomicWithPattern(path string, data []byte, mode os.FileMode, tempPattern string) error {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return errors.New("fsx: write atomic: path is empty")
	}

	dir := filepath.Dir(trimmedPath)
	if err := EnsureDir(dir, defaultDirMode); err != nil {
		return fmt.Errorf("fsx: write atomic %s: %w", trimmedPath, err)
	}

	pattern := tempPattern
	if pattern == "" {
		pattern = defaultTempPattern(trimmedPath)
	}

	temp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("fsx: write atomic %s: create temp file: %w", trimmedPath, err)
	}
	tempPath := temp.Name()

	discard := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}

	if _, err := temp.Write(data); err != nil {
		discard()
		return fmt.Errorf("fsx: write atomic %s: write temp file: %w", trimmedPath, err)
	}
	if err := temp.Chmod(mode); err != nil {
		discard()
		return fmt.Errorf("fsx: write atomic %s: set permissions: %w", trimmedPath, err)
	}
	if err := temp.Sync(); err != nil {
		discard()
		return fmt.Errorf("fsx: write atomic %s: sync temp file: %w", trimmedPath, err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("fsx: write atomic %s: close temp file: %w", trimmedPath, err)
	}
	if err := ReplaceFile(tempPath, trimmedPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("fsx: write atomic %s: %w", trimmedPath, err)
	}

	return nil
}

func defaultTempPattern(path string) string {
	base := filepath.Base(path)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ".fileforge-*.tmp"
	}

	return ".fileforge-" + base + "-*.tmp"
}

// ReplaceFile moves src over dst. On Unix it is a rename. On Windows, when the
// rename fails (for example because the destination is locked), it moves the
// old destination aside, renames src into place, and restores the old file if
// the second rename fails.
func ReplaceFile(src, dst string) error {
	if strings.TrimSpace(src) == "" || strings.TrimSpace(dst) == "" {
		return errors.New("fsx: replace file: source and destination are required")
	}

	renameErr := os.Rename(src, dst)
	if renameErr == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return fmt.Errorf("fsx: replace %s: %w", dst, renameErr)
	}

	backupPath := ""
	if _, statErr := os.Stat(dst); statErr == nil {
		backupPath, statErr = reserveBackupPath(dst)
		if statErr != nil {
			return fmt.Errorf("fsx: replace %s: %w", dst, renameErr)
		}
		if err := os.Rename(dst, backupPath); err != nil {
			_ = os.Remove(backupPath)
			return fmt.Errorf("fsx: replace %s: %w", dst, renameErr)
		}
	}

	if err := os.Rename(src, dst); err != nil {
		if backupPath != "" {
			_ = os.Rename(backupPath, dst)
		}
		return fmt.Errorf("fsx: replace %s: %w", dst, err)
	}
	if backupPath != "" {
		_ = os.Remove(backupPath)
	}

	return nil
}

// reserveBackupPath returns an unused path next to dst that can hold the old
// destination while the replacement is attempted.
func reserveBackupPath(dst string) (string, error) {
	backup, err := os.CreateTemp(filepath.Dir(dst), ".fileforge-replace-*.bak")
	if err != nil {
		return "", err
	}
	path := backup.Name()
	if closeErr := backup.Close(); closeErr != nil {
		_ = os.Remove(path)
		return "", closeErr
	}
	if removeErr := os.Remove(path); removeErr != nil {
		return "", removeErr
	}

	return path, nil
}

// EnsureDir creates path and any missing parents using mode.
func EnsureDir(path string, mode os.FileMode) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("fsx: ensure dir: path is empty")
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("fsx: create directory %s: %w", path, err)
	}

	return nil
}

// UniqueFilePath returns path when it is free, otherwise the first available
// path with a "-1", "-2", ... suffix before the extension. It returns an error
// when every bounded candidate is taken.
func UniqueFilePath(path string) (string, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return "", errors.New("fsx: unique file path: path is empty")
	}
	if !occupied(trimmedPath) {
		return trimmedPath, nil
	}

	ext := filepath.Ext(trimmedPath)
	base := strings.TrimSuffix(trimmedPath, ext)
	for suffix := 1; suffix <= maxUniqueAttempts; suffix++ {
		candidate := fmt.Sprintf("%s-%d%s", base, suffix, ext)
		if !occupied(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("fsx: unique file path: no free name for %s after %d attempts", trimmedPath, maxUniqueAttempts)
}

// occupied reports whether path exists or cannot be inspected. A permission
// error counts as occupied so callers never receive a path that might collide.
func occupied(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// Exists reports whether path exists. Broken symbolic links report false.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir reports whether path exists and is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsRegularFile reports whether path exists and is a regular file. It follows
// symbolic links, so a link to a regular file reports true.
func IsRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// SameFile reports whether a and b resolve to the same file. It prefers the
// operating system identity and falls back to path-key comparison when either
// path cannot be inspected.
func SameFile(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}

	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)
	if aErr == nil && bErr == nil {
		return os.SameFile(aInfo, bInfo)
	}

	return HostPathKey(a) == HostPathKey(b)
}

// HostPathKey returns a host-native path key. Unlike NormalizePathKey, it
// folds case only on Windows, so case-sensitive POSIX names stay distinct.
func HostPathKey(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}

	cleaned := filepath.Clean(trimmed)
	if absolute, err := filepath.Abs(cleaned); err == nil {
		cleaned = absolute
	}
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}

	return cleaned
}

// NormalizePathKey returns a stable map key for a filesystem path. The key is
// platform-independent so callers can compare Windows drive, Windows UNC, and
// POSIX paths on any host: separators are normalized to "/", the value is
// cleaned, resolved to an absolute path when possible, and lower-cased.
func NormalizePathKey(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}

	normalizedSlashes := strings.ReplaceAll(trimmed, "\\", "/")

	if strings.HasPrefix(normalizedSlashes, "//") {
		uncTail := strings.TrimLeft(normalizedSlashes, "/")
		cleanedUNC := pathpkg.Clean("/" + uncTail)
		return "unc://" + strings.ToLower(strings.TrimPrefix(cleanedUNC, "/"))
	}

	if len(normalizedSlashes) >= 2 && normalizedSlashes[1] == ':' {
		drive := strings.ToLower(normalizedSlashes[:1])
		rest := normalizedSlashes[2:]
		cleanedRest := pathpkg.Clean("/" + strings.TrimPrefix(rest, "/"))
		return "win:" + drive + ":" + strings.ToLower(cleanedRest)
	}

	cleaned := pathpkg.Clean(normalizedSlashes)
	if absolute, err := filepath.Abs(cleaned); err == nil {
		cleaned = filepath.ToSlash(absolute)
	}

	return "posix:" + strings.ToLower(cleaned)
}

// WriteProbe proves dir is writable by creating and removing a temporary file.
func WriteProbe(dir string) error {
	trimmedDir := strings.TrimSpace(dir)
	if trimmedDir == "" {
		return errors.New("fsx: write probe: directory is empty")
	}

	info, err := os.Stat(trimmedDir)
	if err != nil {
		return fmt.Errorf("fsx: write probe %s: %w", trimmedDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("fsx: write probe %s: not a directory", trimmedDir)
	}

	probe, err := os.CreateTemp(trimmedDir, ".fileforge-writecheck-*.tmp")
	if err != nil {
		return fmt.Errorf("fsx: write probe %s: %w", trimmedDir, err)
	}
	probePath := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("fsx: write probe %s: %w", trimmedDir, closeErr)
	}
	if removeErr := os.Remove(probePath); removeErr != nil {
		return fmt.Errorf("fsx: write probe %s: %w", trimmedDir, removeErr)
	}

	return nil
}
