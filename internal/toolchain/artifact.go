package toolchain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Artifact identifies one pinned, verifiable download. SHA256 and SizeBytes are
// the reproducibility contract: the installer refuses bytes that do not match
// them. An empty SHA256 skips the checksum comparison (used by the legacy
// Download path and by tests that do not pin a payload), while SizeBytes <= 0
// skips the size comparison.
type Artifact struct {
	URL       string
	SHA256    string
	SizeBytes int64
	FileName  string
}

// Verify checks a downloaded file against the pinned size and SHA-256. A size
// mismatch is reported before hashing, and the checksum comparison is
// case-insensitive so metadata copied from different upstreams is accepted.
// Verify never removes the file: the downloader owns the temporary path and
// cleans it up when verification fails.
func (a Artifact) Verify(path string) error {
	name := filepath.Base(path)

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", name, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", name)
	}
	if a.SizeBytes > 0 && info.Size() != a.SizeBytes {
		return fmt.Errorf("%s has %d bytes, want %d", name, info.Size(), a.SizeBytes)
	}

	expected := normalizeSHA256(a.SHA256)
	if expected == "" {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer func() {
		_ = file.Close()
	}()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash %s: %w", name, err)
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("%s checksum is %s, want %s", name, actual, expected)
	}

	return nil
}

// normalizeSHA256 trims and lowercases a checksum so comparisons do not depend
// on how the upstream published it.
func normalizeSHA256(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
