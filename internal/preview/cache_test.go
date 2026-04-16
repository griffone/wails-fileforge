package preview

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCachePutGetSmall(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "fileforge-previews-test")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	c, err := NewPreviewCache(10*1024*1024, dir, 100*1024*1024, 1024*1024, 5*time.Minute)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	data := []byte("hello")
	if err := c.Put("job-small", data); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok, err := c.Get("job-small")
	if err != nil {
		t.Fatalf("get err: %v", err)
	}
	if !ok {
		t.Fatalf("expected found")
	}
	if string(got) != string(data) {
		t.Fatalf("mismatch: %s", string(got))
	}
}

func TestCacheSpillToDisk(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "fileforge-previews-test")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	c, err := NewPreviewCache(10*1024*1024, dir, 100*1024*1024, 1024, 5*time.Minute)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	// item > spill threshold (1KB)
	data := make([]byte, 2048)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := c.Put("job-big", data); err != nil {
		t.Fatalf("put big: %v", err)
	}
	got, ok, err := c.Get("job-big")
	if err != nil {
		t.Fatalf("get big err: %v", err)
	}
	if !ok {
		t.Fatalf("expected found big")
	}
	if len(got) != len(data) {
		t.Fatalf("len mismatch got=%d want=%d", len(got), len(data))
	}
}
