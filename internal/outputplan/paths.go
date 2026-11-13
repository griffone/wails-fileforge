package outputplan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// maxSuffixAttempts bounds collision probing so an unreadable directory can
// never turn planning into an infinite loop.
const maxSuffixAttempts = 1000

// UniqueFilePath resolves a non-existing file path inside dir using base and
// ext. It tries base.ext, then base-2.ext, base-3.ext and so on. Reserved
// names in used are skipped, and an existing file on disk is never returned.
// The returned bool reports whether the name was adjusted with a suffix.
func UniqueFilePath(dir, base, ext string, used map[string]struct{}) (string, bool) {
	return uniquePath(normalizeDir(dir), normalizeBase(base), NormalizeExtension(ext), used)
}

// UniqueDirPath resolves a non-existing directory path inside dir using base.
// It follows the same suffix policy as UniqueFilePath.
func UniqueDirPath(dir, base string, used map[string]struct{}) (string, bool) {
	return uniquePath(normalizeDir(dir), normalizeBase(base), "", used)
}

// NormalizeExtension lowercases an extension and removes a leading dot.
func NormalizeExtension(ext string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
}

// MarkUsed reserves a path in a used set so later planning calls skip it.
func MarkUsed(used map[string]struct{}, path string) {
	if used == nil {
		return
	}
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return
	}
	used[pathKey(trimmed)] = struct{}{}
}

func uniquePath(dir, base, ext string, used map[string]struct{}) (string, bool) {
	candidate := joinName(dir, base, ext)
	if claim(candidate, used) {
		return candidate, false
	}

	for suffix := 2; suffix <= maxSuffixAttempts; suffix++ {
		candidate = joinName(dir, fmt.Sprintf("%s-%d", base, suffix), ext)
		if claim(candidate, used) {
			return candidate, true
		}
	}

	return joinName(dir, fmt.Sprintf("%s-%d", base, maxSuffixAttempts+1), ext), true
}

func claim(candidate string, used map[string]struct{}) bool {
	key := pathKey(candidate)
	if used != nil {
		if _, reserved := used[key]; reserved {
			return false
		}
	}

	if _, err := os.Stat(candidate); err == nil {
		return false
	} else if !errors.Is(err, os.ErrNotExist) {
		return false
	}

	if used != nil {
		used[key] = struct{}{}
	}
	return true
}

func joinName(dir, base, ext string) string {
	if ext == "" {
		return filepath.Join(dir, base)
	}
	return filepath.Join(dir, base+"."+ext)
}

func normalizeDir(dir string) string {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return "."
	}
	return filepath.Clean(trimmed)
}

func normalizeBase(base string) string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return "output"
	}
	return trimmed
}

func pathKey(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	if runtime.GOOS == "windows" {
		cleaned = strings.ToLower(cleaned)
	}
	return cleaned
}
