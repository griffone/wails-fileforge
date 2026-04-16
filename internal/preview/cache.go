package preview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dgraph-io/ristretto/v2"
)

// PreviewCache provides an in-memory LRU cache with optional disk spill.
type PreviewCache struct {
	mem      *ristretto.Cache[string, any]
	diskDir  string
	spillThr int64
	mu       sync.RWMutex
	maxDisk  int64
	ttl      time.Duration
}

// NewPreviewCache constructs a cache. spillThresholdBytes controls when items are written to disk.
func NewPreviewCache(maxCacheBytes int64, diskDir string, maxDiskBytes int64, spillThresholdBytes int64, ttl time.Duration) (*PreviewCache, error) {
	if maxCacheBytes <= 0 {
		return nil, errors.New("maxCacheBytes must be > 0")
	}
	cfg := &ristretto.Config[string, any]{
		NumCounters: 1e4, // number of keys to track frequency
		MaxCost:     maxCacheBytes,
		BufferItems: 64,
	}
	mem, err := ristretto.NewCache[string, any](cfg)
	if err != nil {
		return nil, fmt.Errorf("preview: new cache: %w", err)
	}
	if diskDir == "" {
		diskDir = filepath.Join(os.TempDir(), "fileforge-previews")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &PreviewCache{mem: mem, diskDir: diskDir, spillThr: spillThresholdBytes, maxDisk: maxDiskBytes, ttl: ttl}, nil
}

// Put stores data in cache; may spill to disk if over threshold.
func (c *PreviewCache) Put(jobID string, data []byte) error {
	if c == nil || c.mem == nil {
		return errors.New("cache not initialized")
	}
	cost := int64(len(data))
	// if larger than spill threshold, write to disk
	if c.spillThr > 0 && cost >= c.spillThr {
		if c.diskDir == "" {
			// no disk configured: store in memory anyway
			c.mem.SetWithTTL(jobID, data, cost, c.ttl)
			c.mem.Wait()
			return nil
		}
		path, err := WriteSpillFile(c.diskDir, jobID, data)
		if err != nil {
			return fmt.Errorf("preview: spill write: %w", err)
		}
		// store sentinel path in memcache
		c.mem.SetWithTTL(jobID, path, int64(len(path)), c.ttl)
		c.mem.Wait()
		return nil
	}
	c.mem.SetWithTTL(jobID, data, cost, c.ttl)
	c.mem.Wait()
	return nil
}

// Get returns data and contentType if present. For spill files, reads disk file.
func (c *PreviewCache) Get(jobID string) ([]byte, bool, error) {
	if c == nil || c.mem == nil {
		return nil, false, errors.New("cache not initialized")
	}
	v, ok := c.mem.Get(jobID)
	if !ok {
		return nil, false, nil
	}
	switch v := v.(type) {
	case []byte:
		return v, true, nil
	case string:
		// treat as path to spill file
		data, err := os.ReadFile(v)
		if err != nil {
			return nil, false, fmt.Errorf("preview: read spill: %w", err)
		}
		return data, true, nil
	default:
		return nil, false, fmt.Errorf("preview: unknown cache entry type")
	}
}

// Delete removes an entry from cache and removes spill file if present.
func (c *PreviewCache) Delete(jobID string) {
	if c == nil || c.mem == nil {
		return
	}
	v, ok := c.mem.Get(jobID)
	if ok {
		if path, ok := v.(string); ok {
			_ = RemoveSpillFile(path)
		}
	}
	c.mem.Del(jobID)
}
