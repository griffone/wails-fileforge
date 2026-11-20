package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// MigrateFile copies oldPath to newPath when newPath does not exist yet.
//
// The legacy file is left in place: migration never deletes user data. When
// newPath already exists it is untouched, and when neither path exists the
// call is a no-op. The copy is published atomically with WriteAtomic.
func MigrateFile(oldPath, newPath string, mode os.FileMode) error {
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if oldPath == "" || newPath == "" {
		return nil
	}

	if _, err := os.Stat(newPath); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("fsx: migrate %s: stat destination: %w", newPath, err)
	}

	payload, err := os.ReadFile(oldPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("fsx: migrate %s: read legacy file: %w", oldPath, err)
	}

	if err := WriteAtomic(newPath, payload, mode); err != nil {
		return fmt.Errorf("fsx: migrate %s: %w", newPath, err)
	}

	return nil
}
