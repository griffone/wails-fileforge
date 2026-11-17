package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// applyManagedEnvironment prepends the managed toolchain directories to PATH
// and sets TESSDATA_PREFIX. Managed entries already present in PATH are
// removed first, so repeated calls are idempotent.
func applyManagedEnvironment(root, goos string, getenv func(string) string, setenv func(string, string) error) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("the toolchain directory is not configured")
	}
	if setenv == nil {
		return fmt.Errorf("the environment setter is not configured")
	}

	pathValue := ""
	if getenv != nil {
		pathValue = getenv("PATH")
	}
	separator := pathListSeparatorFor(goos)
	updatedPath := prependPath(pathValue, managedPathDirs(root, goos), separator)

	if err := setenv("PATH", updatedPath); err != nil {
		return fmt.Errorf("update PATH: %w", err)
	}

	// Point TESSDATA_PREFIX at the managed language data only once it exists: a
	// first run before the install completes must not hide a system Tesseract
	// installation.
	tessdata := tessdataDir(root)
	if directoryExists(tessdata) {
		if err := setenv("TESSDATA_PREFIX", tessdata); err != nil {
			return fmt.Errorf("set TESSDATA_PREFIX: %w", err)
		}
	}

	return nil
}

// pathListSeparatorFor returns the PATH separator for a GOOS value so tests can
// exercise the Windows layout on any host.
func pathListSeparatorFor(goos string) string {
	if goos == "windows" {
		return ";"
	}

	return string(os.PathListSeparator)
}

// prependPath returns dirs followed by the current PATH entries that are not
// managed directories. Duplicate managed directories appear once.
func prependPath(current string, dirs []string, separator string) string {
	managed := make(map[string]struct{}, len(dirs))
	ordered := make([]string, 0, len(dirs)+1)
	for _, dir := range dirs {
		cleaned := filepath.Clean(dir)
		if _, exists := managed[cleaned]; exists {
			continue
		}

		managed[cleaned] = struct{}{}
		ordered = append(ordered, cleaned)
	}

	existing := make([]string, 0, 8)
	if strings.TrimSpace(current) != "" {
		for _, entry := range strings.Split(current, separator) {
			if entry == "" {
				continue
			}
			if _, isManaged := managed[filepath.Clean(entry)]; isManaged {
				continue
			}

			existing = append(existing, entry)
		}
	}

	parts := append(ordered, existing...)

	return strings.Join(parts, separator)
}
