package preview

import (
	"os"
	"path/filepath"
	"time"
)

// StartJanitor starts a background goroutine that periodically cleans up disk spill files
// older than diskTTL or when disk usage exceeds maxDiskBytes.
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
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			// Evict files older than diskTTL and ensure disk usage <= maxDisk
			if c.diskDir == "" {
				continue
			}
			// list files
			files, err := os.ReadDir(c.diskDir)
			if err != nil {
				continue
			}
			now := time.Now()
			// first pass: remove files older than diskTTL
			for _, f := range files {
				info, err := f.Info()
				if err != nil {
					continue
				}
				if now.Sub(info.ModTime()) > diskTTL {
					_ = RemoveSpillFile(filepath.Join(c.diskDir, f.Name()))
					// log redacted eviction (jobID is filename prefix)
				}
			}
			// second pass: if disk usage still > maxDisk, evict oldest until under
			used, err := DiskUsage(c.diskDir)
			if err != nil {
				continue
			}
			if used <= c.maxDisk {
				continue
			}
			// build list of files sorted by modtime ascending
			var entries []os.FileInfo
			for _, f := range files {
				info, err := f.Info()
				if err == nil {
					entries = append(entries, info)
				}
			}
			// simple selection sort by modtime
			for i := 0; i < len(entries)-1; i++ {
				for j := i + 1; j < len(entries); j++ {
					if entries[j].ModTime().Before(entries[i].ModTime()) {
						entries[i], entries[j] = entries[j], entries[i]
					}
				}
			}
			// evict until under limit
			for _, e := range entries {
				if used <= c.maxDisk {
					break
				}
				path := filepath.Join(c.diskDir, e.Name())
				size := e.Size()
				if err := RemoveSpillFile(path); err == nil {
					used -= size
					// log redacted eviction: jobID derived from filename
				}
			}
		}
	}()
}
