package preview

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureDiskDir ensures the diskDir exists with secure permissions and returns the path.
func EnsureDiskDir(dir string) (string, error) {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "fileforge-previews")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("preview: ensure disk dir: %w", err)
	}
	return dir, nil
}

// WriteSpillFile writes data to a file named by jobID under dir and returns the full path.
func WriteSpillFile(dir, jobID string, data []byte) (string, error) {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "fileforge-previews")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("preview: write spill mkdir: %w", err)
	}
	// use jobID as filename safe component
	fname := filepath.Join(dir, jobID+cacheEntrySuffix)
	// write to temp file first then rename
	tmp, err := os.CreateTemp(dir, "spill-*")
	if err != nil {
		return "", fmt.Errorf("preview: tmpfile: %w", err)
	}
	// ensure secure perms
	if err := tmp.Chmod(0o600); err != nil {
		// ignore chmod error
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("preview: write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("preview: close tmp: %w", err)
	}
	if err := os.Rename(tmp.Name(), fname); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("preview: rename spill: %w", err)
	}
	return fname, nil
}

// WriteCacheEntry writes data and a small meta file containing the mime type
// to the cache directory under a stable filename derived from keyBase.
func WriteCacheEntry(dir, keyBase string, data []byte, contentType string) (string, error) {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "fileforge-previews")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("preview: write cache mkdir: %w", err)
	}
	fname := filepath.Join(dir, keyBase+cacheEntrySuffix)
	// write to temp then rename
	tmp, err := os.CreateTemp(dir, "cache-*")
	if err != nil {
		return "", fmt.Errorf("preview: tmpfile: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		// ignore chmod error
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("preview: write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("preview: close tmp: %w", err)
	}
	if err := os.Rename(tmp.Name(), fname); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("preview: rename cache: %w", err)
	}
	// write meta file
	metaPath := fname + ".meta"
	if err := os.WriteFile(metaPath, []byte(contentType), 0o600); err != nil {
		// best effort: ignore error but log in future
	}
	return fname, nil
}

// RemoveSpillFile removes the spill file at path and its .meta sidecar if they
// exist.
func RemoveSpillFile(path string) error {
	if path == "" {
		return nil
	}

	var firstErr error
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		firstErr = fmt.Errorf("preview: remove spill: %w", err)
	}
	if err := os.Remove(path + ".meta"); err != nil && !os.IsNotExist(err) && firstErr == nil {
		firstErr = fmt.Errorf("preview: remove spill meta: %w", err)
	}

	return firstErr
}

// DiskUsage computes total bytes used by .bin spill files under dir. Metadata
// sidecars do not count against the disk cap.
func DiskUsage(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("preview: disk usage read: %w", err)
	}

	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), cacheEntrySuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}

	return total, nil
}
