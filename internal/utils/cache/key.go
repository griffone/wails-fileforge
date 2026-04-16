package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type PageRange struct {
	Start int
	End   int
}

// GeneratePreviewCacheKey deterministically builds a cache key for preview
// outputs based on file identity, requested page range and rendering params.
func GeneratePreviewCacheKey(fileIDOrPath string, modTime int64, fileSize int64, pr PageRange, offset, width, height int, format string, quality int) string {
	// Build a canonical string representation
	s := fmt.Sprintf("%s|%d|%d|%d-%d|%d|%d|%d|%s|%d", fileIDOrPath, modTime, fileSize, pr.Start, pr.End, offset, width, height, format, quality)
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
