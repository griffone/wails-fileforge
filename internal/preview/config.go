package preview

import "time"

// Config keys and defaults for preview subsystem
const (
	// Default settings used when constructing PreviewService
	DefaultMaxQueue        = 1024
	DefaultJobTTL          = 10 * time.Minute
	DefaultCleanupInterval = 1 * time.Minute
	DefaultJobTimeout      = 30 * time.Second
)
