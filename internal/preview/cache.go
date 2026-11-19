package preview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto/v2"
)

const cacheEntrySuffix = ".bin"

// errCacheAdmission reports that the cache rejected an item instead of storing
// it. Callers may treat it as a soft failure: the preview still works, it is
// just not cached.
var errCacheAdmission = errors.New("preview: cache admission rejected")

// cacheEntry is the typed value stored in the memory cache. Memory entries keep
// their bytes and MIME type; spilled entries keep the spill path and MIME type.
type cacheEntry struct {
	Data []byte
	MIME string
	Path string
}

// diskEntry describes one spill file on disk.
type diskEntry struct {
	path    string
	key     string
	size    int64
	modTime time.Time
}

// PreviewCache provides an in-memory LRU cache with optional disk spill.
type PreviewCache struct {
	mem      *ristretto.Cache[string, cacheEntry]
	diskDir  string
	spillThr int64
	maxDisk  int64
	maxMem   int64
	ttl      time.Duration

	// mu serializes cache mutations, disk admission, and Close. Reads take the
	// read lock so Close never races with an in-flight ristretto operation.
	mu sync.RWMutex

	janitorMu   sync.Mutex
	janitorStop chan struct{}
	janitorDone chan struct{}
	closeOnce   sync.Once
}

// NewPreviewCache constructs a cache. spillThresholdBytes controls when items are written to disk.
func NewPreviewCache(maxCacheBytes int64, diskDir string, maxDiskBytes int64, spillThresholdBytes int64, ttl time.Duration) (*PreviewCache, error) {
	if maxCacheBytes <= 0 {
		return nil, errors.New("maxCacheBytes must be > 0")
	}
	cfg := &ristretto.Config[string, cacheEntry]{
		NumCounters: 1e4, // number of keys to track frequency
		MaxCost:     maxCacheBytes,
		BufferItems: 64,
	}
	mem, err := ristretto.NewCache[string, cacheEntry](cfg)
	if err != nil {
		return nil, fmt.Errorf("preview: new cache: %w", err)
	}
	if diskDir == "" {
		diskDir = filepath.Join(os.TempDir(), "fileforge-previews")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &PreviewCache{
		mem:      mem,
		diskDir:  diskDir,
		spillThr: spillThresholdBytes,
		maxDisk:  maxDiskBytes,
		maxMem:   maxCacheBytes,
		ttl:      ttl,
	}, nil
}

// Put stores data in cache; may spill to disk if over threshold.
// contentType is the MIME type for the data (e.g., image/webp).
func (c *PreviewCache) Put(jobID string, data []byte, contentType string) error {
	if c == nil || c.mem == nil {
		return errors.New("cache not initialized")
	}

	spill := c.spillThr > 0 && int64(len(data)) >= c.spillThr && c.diskDir != ""
	if !spill {
		cost := int64(len(data))
		if cost <= 0 {
			cost = 1
		}
		if c.maxMem > 0 && cost > c.maxMem {
			return fmt.Errorf("%w: item cost %d exceeds memory limit %d", errCacheAdmission, cost, c.maxMem)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if admitted := c.mem.SetWithTTL(jobID, cacheEntry{Data: data, MIME: contentType}, cost, c.ttl); !admitted {
			return fmt.Errorf("%w: memory entry was dropped", errCacheAdmission)
		}
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.enforceDiskCapacityLocked(int64(len(data))); err != nil {
		return err
	}
	path, err := WriteCacheEntry(c.diskDir, jobID, data, contentType)
	if err != nil {
		return fmt.Errorf("preview: spill write: %w", err)
	}
	cost := int64(len(path) + len(contentType))
	if cost <= 0 {
		cost = 1
	}
	if c.maxMem > 0 && cost > c.maxMem {
		_ = RemoveSpillFile(path)
		return fmt.Errorf("%w: item cost %d exceeds memory limit %d", errCacheAdmission, cost, c.maxMem)
	}
	if admitted := c.mem.SetWithTTL(jobID, cacheEntry{MIME: contentType, Path: path}, cost, c.ttl); !admitted {
		_ = RemoveSpillFile(path)
		return fmt.Errorf("%w: spill entry was dropped", errCacheAdmission)
	}

	return nil
}

// Get returns data and contentType if present. For spill files, reads the disk
// file and returns the MIME type stored with the entry.
func (c *PreviewCache) Get(jobID string) ([]byte, string, bool, error) {
	if c == nil || c.mem == nil {
		return nil, "", false, errors.New("cache not initialized")
	}
	c.mu.RLock()
	entry, ok := c.mem.Get(jobID)
	c.mu.RUnlock()
	if !ok {
		return nil, "", false, nil
	}
	if entry.Path != "" {
		data, err := os.ReadFile(entry.Path)
		if err != nil {
			return nil, "", false, fmt.Errorf("preview: read spill: %w", err)
		}
		return data, entry.MIME, true, nil
	}

	return entry.Data, entry.MIME, true, nil
}

// Wait blocks until pending cache writes have been processed. Tests use it for
// read-after-write determinism; the write path never waits.
func (c *PreviewCache) Wait() {
	if c == nil || c.mem == nil {
		return
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.mem.Wait()
}

// Delete removes an entry from cache and removes its spill file and metadata
// sidecar if present.
func (c *PreviewCache) Delete(jobID string) {
	if c == nil || c.mem == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.mem.Get(jobID); ok && entry.Path != "" {
		_ = RemoveSpillFile(entry.Path)
	}
	// The spill path is derived from the key, so remove it even when the
	// asynchronous admission has not published the entry yet.
	if c.diskDir != "" {
		_ = RemoveSpillFile(filepath.Join(c.diskDir, jobID+cacheEntrySuffix))
	}
	c.mem.Del(jobID)
}

// Close stops the cache janitor and closes the memory cache. It is safe to call
// more than once.
func (c *PreviewCache) Close() {
	if c == nil {
		return
	}
	// Hold the write lock so no Put/Get/Wait is in flight while ristretto shuts
	// down. StopJanitor waits for the janitor goroutine, which does not take
	// this lock.
	c.mu.Lock()
	defer c.mu.Unlock()
	c.StopJanitor()
	c.closeOnce.Do(func() {
		if c.mem != nil {
			c.mem.Close()
		}
	})
}

// enforceDiskCapacityLocked evicts the oldest spill files until the new item
// fits inside maxDisk. The caller must hold c.mu.
func (c *PreviewCache) enforceDiskCapacityLocked(needed int64) error {
	if c.maxDisk <= 0 || c.diskDir == "" {
		return nil
	}

	entries, err := listDiskEntries(c.diskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("preview: list spill files: %w", err)
	}
	used := diskEntriesSize(entries)
	if used+needed <= c.maxDisk {
		return nil
	}

	sortDiskEntriesOldestFirst(entries)
	for _, entry := range entries {
		if used+needed <= c.maxDisk {
			break
		}
		c.evictDiskEntry(entry)
		used -= entry.size
	}
	if used+needed > c.maxDisk {
		return fmt.Errorf("preview: spill of %d bytes exceeds disk cap %d", needed, c.maxDisk)
	}

	return nil
}

// evictDiskEntry removes a spill file, its sidecar, and the matching memory
// index entry.
func (c *PreviewCache) evictDiskEntry(entry diskEntry) {
	_ = RemoveSpillFile(entry.path)
	if c.mem != nil {
		c.mem.Del(entry.key)
	}
}

// listDiskEntries returns the .bin spill files under dir. Metadata sidecars and
// other files are ignored.
func listDiskEntries(dir string) ([]diskEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	result := make([]diskEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), cacheEntrySuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, diskEntry{
			path:    filepath.Join(dir, entry.Name()),
			key:     strings.TrimSuffix(entry.Name(), cacheEntrySuffix),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}

	return result, nil
}

func diskEntriesSize(entries []diskEntry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.size
	}

	return total
}

func sortDiskEntriesOldestFirst(entries []diskEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime.Before(entries[j].modTime)
	})
}
