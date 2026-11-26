package models

// DuplicateScanRequest describes a read-only duplicate scan. It is not a job
// request: the inputs are folders, not files, and nothing is ever written.
type DuplicateScanRequest struct {
	// Directories are the folders to scan. The same folder listed twice (or a
	// folder that contains another requested folder) is scanned once.
	Directories []string `json:"directories"`
	// Recursive walks subfolders. Omitted means true.
	Recursive *bool `json:"recursive,omitempty"`
	// MinSizeBytes skips files smaller than this. Zero uses the service default
	// (4 KiB); a negative value skips only empty files.
	MinSizeBytes int64 `json:"minSizeBytes,omitempty"`
	// Extensions limits candidates to the listed extensions. Entries may be
	// written with or without the leading dot; an empty list accepts any file.
	Extensions []string `json:"extensions,omitempty"`
	// MaxFiles bounds how many files one scan records. Zero uses the default; a
	// negative value disables the cap.
	MaxFiles int `json:"maxFiles,omitempty"`
	// Workers is the hashing concurrency. Zero uses the service default.
	Workers int `json:"workers,omitempty"`
}

// DuplicateScanGroup is one set of files with identical content. Paths are
// sorted; WastedBytes is the space recoverable by keeping a single copy.
type DuplicateScanGroup struct {
	Hash        string   `json:"hash"`
	SizeBytes   int64    `json:"sizeBytes"`
	Count       int      `json:"count"`
	WastedBytes int64    `json:"wastedBytes"`
	Paths       []string `json:"paths"`
}

// DuplicateScanTotals summarises one scan. FilesScanned counts the files that
// passed the name, extension and size filters; FilesHashed counts the subset
// that the hashing phase opened (including files that turned out unreadable).
type DuplicateScanTotals struct {
	FilesScanned int   `json:"filesScanned"`
	FilesHashed  int   `json:"filesHashed"`
	Groups       int   `json:"groups"`
	WastedBytes  int64 `json:"wastedBytes"`
}

// DuplicateScanWarning is a non-fatal problem: an unreadable file or folder, or
// a cap that stopped the walk. Path is empty for scan-wide warnings.
type DuplicateScanWarning struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// DuplicateScanProgressEvent is emitted on duplicates/progress. Its shape
// mirrors JobProgressEvent so the activity drawer can merge it the same way;
// ScanID replaces JobID because the work is not a job-engine job.
type DuplicateScanProgressEvent struct {
	ScanID   string      `json:"scanId"`
	Kind     string      `json:"kind,omitempty"`
	Status   string      `json:"status"`
	Progress JobProgress `json:"progress"`
}

// DuplicateScanState is the full snapshot emitted on duplicates/state and
// returned by GetScan. Status uses the shared job status vocabulary so terminal
// states (success, cancelled, failed) render like any other background task.
type DuplicateScanState struct {
	ScanID    string                 `json:"scanId"`
	Kind      string                 `json:"kind,omitempty"`
	Success   bool                   `json:"success"`
	Message   string                 `json:"message"`
	Status    string                 `json:"status"`
	Progress  JobProgress            `json:"progress"`
	Groups    []DuplicateScanGroup   `json:"groups"`
	Totals    DuplicateScanTotals    `json:"totals"`
	Warnings  []DuplicateScanWarning `json:"warnings"`
	StartedAt int64                  `json:"startedAt"`
	EndedAt   int64                  `json:"endedAt,omitempty"`
}

// DuplicateStartResponse is returned when a scan is accepted.
type DuplicateStartResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	ScanID  string `json:"scanId,omitempty"`
}

// DuplicateScanStatusResponse returns one scan snapshot.
type DuplicateScanStatusResponse struct {
	Success bool                `json:"success"`
	Message string              `json:"message"`
	Found   bool                `json:"found"`
	Scan    *DuplicateScanState `json:"scan,omitempty"`
}

// DuplicateScanListResponse lists retained scans, newest first.
type DuplicateScanListResponse struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	Scans   []DuplicateScanState `json:"scans"`
}

// DuplicateScanActionResponse is the result of cancelling a scan.
type DuplicateScanActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	ScanID  string `json:"scanId,omitempty"`
}
