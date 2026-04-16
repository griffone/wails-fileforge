package preview

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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
	fname := filepath.Join(dir, jobID+".bin")
	// write to temp file first then rename
	tmp, err := ioutil.TempFile(dir, "spill-*")
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

// RemoveSpillFile removes the spill file at path if it exists.
func RemoveSpillFile(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("preview: remove spill: %w", err)
	}
	return nil
}

// DiskUsage computes total bytes used under dir (non-recursive for spill files).
func DiskUsage(dir string) (int64, error) {
	f, err := os.Open(dir)
	if err != nil {
		return 0, fmt.Errorf("preview: disk usage open: %w", err)
	}
	defer f.Close()
	entries, err := f.Readdir(-1)
	if err != nil {
		return 0, fmt.Errorf("preview: disk usage read: %w", err)
	}
	var total int64
	for _, e := range entries {
		if !e.IsDir() {
			total += e.Size()
		}
	}
	return total, nil
}
