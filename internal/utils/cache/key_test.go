package cache

import "testing"

func TestGeneratePreviewCacheKeyDeterministic(t *testing.T) {
	k1 := GeneratePreviewCacheKey("/tmp/foo.pdf", 1610000000, 12345, PageRange{Start: 1, End: 1}, 0, 128, 128, "webp", 80)
	k2 := GeneratePreviewCacheKey("/tmp/foo.pdf", 1610000000, 12345, PageRange{Start: 1, End: 1}, 0, 128, 128, "webp", 80)
	if k1 != k2 {
		t.Fatalf("keys differ: %s != %s", k1, k2)
	}
	// Ensure length matches sha256 hex (64)
	if len(k1) != 64 {
		t.Fatalf("unexpected key length: %d", len(k1))
	}
}
