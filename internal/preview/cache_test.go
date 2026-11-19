package preview

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestCache(t *testing.T, maxMem, maxDisk, spillThreshold int64) *PreviewCache {
	t.Helper()

	cache, err := NewPreviewCache(maxMem, t.TempDir(), maxDisk, spillThreshold, time.Minute)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	t.Cleanup(cache.Close)

	return cache
}

func TestPreviewCacheMemoryEntryKeepsMIME(t *testing.T) {
	cache := newTestCache(t, 1<<20, 1<<20, 1<<10)
	data := []byte("memory-entry")

	if err := cache.Put("memory-key", data, "image/webp"); err != nil {
		t.Fatalf("put: %v", err)
	}
	cache.Wait()

	got, mime, ok, err := cache.Get("memory-key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatal("expected the memory entry to be present")
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: got %q", got)
	}
	if mime != "image/webp" {
		t.Fatalf("expected the memory entry to keep its MIME type, got %q", mime)
	}
}

func TestPreviewCacheSpillEntryKeepsMIMEAndDeletesMeta(t *testing.T) {
	cache := newTestCache(t, 1<<20, 1<<20, 1)
	data := bytes.Repeat([]byte("s"), 64)

	if err := cache.Put("spill-key", data, "image/jpeg"); err != nil {
		t.Fatalf("put: %v", err)
	}
	cache.Wait()

	got, mime, ok, err := cache.Get("spill-key")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatal("expected the spill entry to be present")
	}
	if !bytes.Equal(got, data) {
		t.Fatal("spill data mismatch")
	}
	if mime != "image/jpeg" {
		t.Fatalf("expected the spill entry to keep its MIME type, got %q", mime)
	}

	binPath := filepath.Join(cache.diskDir, "spill-key"+cacheEntrySuffix)
	metaPath := binPath + ".meta"
	for _, path := range []string{binPath, metaPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist before delete: %v", path, err)
		}
	}

	cache.Delete("spill-key")
	for _, path := range []string{binPath, metaPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, got %v", path, err)
		}
	}
}

func TestPreviewCacheRejectsOversizedMemoryEntry(t *testing.T) {
	cache, err := NewPreviewCache(8, t.TempDir(), 1<<20, 1<<20, time.Minute)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	defer cache.Close()

	err = cache.Put("too-big", make([]byte, 64), "image/webp")
	if !errors.Is(err, errCacheAdmission) {
		t.Fatalf("expected an admission error, got %v", err)
	}
}

func TestPreviewCacheRejectedAdmissionFromClosedCache(t *testing.T) {
	cache, err := NewPreviewCache(1<<20, t.TempDir(), 1<<20, 1<<20, time.Minute)
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	cache.Close()

	err = cache.Put("closed-key", []byte("data"), "image/webp")
	if !errors.Is(err, errCacheAdmission) {
		t.Fatalf("expected the boolean SetWithTTL result to become an error, got %v", err)
	}
}

func TestPreviewCacheEvictsOldestSpillToHonorDiskCap(t *testing.T) {
	cache := newTestCache(t, 1<<20, 300, 1)
	older := bytes.Repeat([]byte("a"), 200)
	newer := bytes.Repeat([]byte("b"), 200)

	if err := cache.Put("older", older, "image/png"); err != nil {
		t.Fatalf("put older: %v", err)
	}
	cache.Wait()
	olderPath := filepath.Join(cache.diskDir, "older"+cacheEntrySuffix)
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(olderPath, oldTime, oldTime); err != nil {
		t.Fatalf("set older mtime: %v", err)
	}

	if err := cache.Put("newer", newer, "image/png"); err != nil {
		t.Fatalf("put newer: %v", err)
	}
	cache.Wait()

	if _, err := os.Stat(olderPath); !os.IsNotExist(err) {
		t.Fatalf("expected the oldest spill file to be evicted, got %v", err)
	}
	if _, err := os.Stat(olderPath + ".meta"); !os.IsNotExist(err) {
		t.Fatalf("expected the oldest spill metadata to be evicted, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache.diskDir, "newer"+cacheEntrySuffix)); err != nil {
		t.Fatalf("expected the newest spill file to remain: %v", err)
	}
	if _, _, ok, err := cache.Get("older"); err != nil || ok {
		t.Fatalf("expected the evicted entry to leave the memory index, ok=%v err=%v", ok, err)
	}

	usage, err := DiskUsage(cache.diskDir)
	if err != nil {
		t.Fatalf("disk usage: %v", err)
	}
	if usage > 300 {
		t.Fatalf("expected disk usage <= 300 bytes, got %d", usage)
	}
}

func TestPreviewCacheCloseStopsJanitor(t *testing.T) {
	cache := newTestCache(t, 1<<20, 1<<20, 1<<10)
	cache.StartJanitor(time.Hour, time.Hour)

	cache.janitorMu.Lock()
	running := cache.janitorStop != nil
	cache.janitorMu.Unlock()
	if !running {
		t.Fatal("expected the janitor to be running")
	}

	cache.Close()

	cache.janitorMu.Lock()
	running = cache.janitorStop != nil
	cache.janitorMu.Unlock()
	if running {
		t.Fatal("expected Close to stop the janitor")
	}

	// Repeated shutdown calls must stay safe.
	cache.StopJanitor()
	cache.Close()
}

func TestPreviewCacheCloseIsSafeWithConcurrentAccess(t *testing.T) {
	cache := newTestCache(t, 1<<20, 1<<20, 1)
	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			_ = cache.Put("key", []byte("data"), "image/png")
			_, _, _, _ = cache.Get("key")
			cache.Wait()
		}
	}()

	cache.Close()
	close(done)
	wg.Wait()
}

func TestDiskUsageCountsOnlySpillFiles(t *testing.T) {
	dir := t.TempDir()
	files := map[string]int{
		"entry.bin":      120,
		"entry.bin.meta": 30,
		"notes.txt":      90,
	}
	for name, size := range files {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	usage, err := DiskUsage(dir)
	if err != nil {
		t.Fatalf("disk usage: %v", err)
	}
	if usage != 120 {
		t.Fatalf("expected only .bin files to count, got %d", usage)
	}
}
