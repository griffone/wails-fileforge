package automation

import (
	"log"
	"path/filepath"

	"fileforge-desktop/internal/platform/fsx"
)

// migrateLegacyFile copies a versioned store file to its unversioned name when
// path uses the current filename. The legacy file is kept so no user data is
// lost; a missing legacy file is a no-op.
func migrateLegacyFile(path, currentName, legacyName string) {
	if filepath.Base(path) != currentName {
		return
	}

	legacyPath := filepath.Join(filepath.Dir(path), legacyName)
	if err := fsx.MigrateFile(legacyPath, path, filePermissions); err != nil {
		log.Printf("automation.migration.failed path=%s err=%v", path, err)
	}
}
