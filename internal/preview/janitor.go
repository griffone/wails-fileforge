package preview

import (
	"time"
)

// StartJanitor starts a background goroutine that periodically cleans up disk
// spill files older than diskTTL or when disk usage exceeds maxDiskBytes.
// Calling StartJanitor while a janitor is already running is a no-op.
func (c *PreviewCache) StartJanitor(diskTTL time.Duration, interval time.Duration) {
	if c == nil {
		return
	}
	if diskTTL <= 0 {
		diskTTL = 15 * time.Minute
	}
	if interval <= 0 {
		interval = 1 * time.Minute
	}

	c.janitorMu.Lock()
	if c.janitorStop != nil {
		c.janitorMu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	c.janitorStop = stop
	c.janitorDone = done
	c.janitorMu.Unlock()

	go c.runJanitor(stop, done, diskTTL, interval)
}

// StopJanitor stops the background janitor and waits for it to exit. It is safe
// to call more than once.
func (c *PreviewCache) StopJanitor() {
	if c == nil {
		return
	}

	c.janitorMu.Lock()
	stop := c.janitorStop
	done := c.janitorDone
	c.janitorStop = nil
	c.janitorDone = nil
	c.janitorMu.Unlock()

	if stop != nil {
		close(stop)
	}
	if done != nil {
		<-done
	}
}

func (c *PreviewCache) runJanitor(stop <-chan struct{}, done chan<- struct{}, diskTTL, interval time.Duration) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanupDisk(diskTTL)
		case <-stop:
			return
		}
	}
}

// cleanupDisk removes expired spill files and then enforces the disk cap on a
// fresh directory listing.
func (c *PreviewCache) cleanupDisk(diskTTL time.Duration) {
	if c.diskDir == "" {
		return
	}

	entries, err := listDiskEntries(c.diskDir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, entry := range entries {
		if now.Sub(entry.modTime) > diskTTL {
			c.evictDiskEntry(entry)
		}
	}

	// Re-list after deletions so the capacity pass is not based on a stale
	// listing.
	entries, err = listDiskEntries(c.diskDir)
	if err != nil || c.maxDisk <= 0 {
		return
	}
	used := diskEntriesSize(entries)
	if used <= c.maxDisk {
		return
	}

	sortDiskEntriesOldestFirst(entries)
	for _, entry := range entries {
		if used <= c.maxDisk {
			break
		}
		c.evictDiskEntry(entry)
		used -= entry.size
	}
}
