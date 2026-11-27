package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fileforge-desktop/internal/platform/fsx"
)

const (
	auditFileName = "mcp_audit.log"
	auditFileMode = 0o600
)

// AuditEntry is one line of mcp_audit.log. It records what ran, where the
// outputs went and how it ended. It never records file contents or option
// values.
type AuditEntry struct {
	Time       time.Time `json:"time"`
	Tool       string    `json:"tool"`
	InputCount int       `json:"inputCount"`
	OutputDir  string    `json:"outputDir"`
	Status     string    `json:"status"`
	ErrorCode  string    `json:"errorCode,omitempty"`
}

// AuditLog appends one JSON line per run. Appends are serialized and each call
// opens the file with O_APPEND so a crash cannot leave a partial earlier line
// rewritten.
type AuditLog struct {
	path string
	mu   sync.Mutex
}

// NewAuditLog creates an audit log at path. An empty path disables auditing.
func NewAuditLog(path string) *AuditLog {
	return &AuditLog{path: path}
}

// Append writes one line. A missing parent directory is created. The caller
// treats a failure as best-effort: a completed job is never reported as failed
// just because the audit line could not be written.
func (a *AuditLog) Append(entry AuditEntry) error {
	if a == nil || a.path == "" {
		return nil
	}
	if entry.Time.IsZero() {
		entry.Time = time.Now().UTC()
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode audit entry: %w", err)
	}
	payload = append(payload, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := fsx.EnsureDir(filepath.Dir(a.path), 0o755); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}

	file, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, auditFileMode)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer func() { _ = file.Close() }()

	if _, err := file.Write(payload); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}

	return nil
}
