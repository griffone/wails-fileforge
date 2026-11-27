package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"fileforge-desktop/internal/models"
)

const (
	exitSuccess    = 0
	exitInternal   = 1
	exitValidation = 2
	exitJobFailed  = 3
)

// outputEntry is one produced path and its size in bytes. A directory size is
// the total size of the files inside it.
type outputEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

// runReport is the single JSON object a run prints on stdout. Message and
// Verification are populated for recipe runs, where the result explains the
// outcome beyond the produced paths.
type runReport struct {
	JobID        string                     `json:"jobId"`
	Status       string                     `json:"status"`
	Success      bool                       `json:"success"`
	Message      string                     `json:"message,omitempty"`
	Outputs      []outputEntry              `json:"outputs"`
	Verification *models.VerificationReport `json:"verification,omitempty"`
	Error        *models.JobError           `json:"error,omitempty"`
}

// exitCodeForStatus maps a terminal job or recipe status to the documented CLI
// exit code.
func exitCodeForStatus(status string) int {
	switch status {
	case models.JobStatusSuccess:
		return exitSuccess
	case models.JobStatusPartialSuccess,
		models.JobStatusFailed,
		models.JobStatusCancelled,
		models.JobStatusInterrupted:
		return exitJobFailed
	default:
		return exitInternal
	}
}

func isTerminalStatus(status string) bool {
	switch status {
	case models.JobStatusSuccess,
		models.JobStatusPartialSuccess,
		models.JobStatusFailed,
		models.JobStatusCancelled,
		models.JobStatusInterrupted:
		return true
	default:
		return false
	}
}

func writeJSON(w io.Writer, value any) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

// writeError prints the most specific canonical code and the message to
// stderr. It mirrors how the UI resolves an error: detail code first.
func writeError(w io.Writer, jobErr *models.JobError) {
	if jobErr == nil {
		fmt.Fprintln(w, "ERROR_UNCLASSIFIED: unknown error")
		return
	}

	code := jobErr.DetailCode
	if code == "" {
		code = jobErr.Code
	}
	if code == "" {
		code = "ERROR_UNCLASSIFIED"
	}
	fmt.Fprintf(w, "%s: %s\n", code, jobErr.Message)
}

// writeFailure reports a validation or internal failure in the requested
// format and returns the supplied exit code.
func writeFailure(stdout, stderr io.Writer, asJSON bool, exitCode int, jobErr *models.JobError) int {
	if asJSON {
		writeJSON(stdout, runReport{
			Status:  models.JobStatusFailed,
			Success: false,
			Outputs: []outputEntry{},
			Error:   jobErr,
		})
		return exitCode
	}

	writeError(stderr, jobErr)
	return exitCode
}

// emitRun prints the terminal report and returns its exit code.
func emitRun(stdout, stderr io.Writer, asJSON bool, report runReport) int {
	if report.Outputs == nil {
		report.Outputs = []outputEntry{}
	}

	if asJSON {
		writeJSON(stdout, report)
	} else {
		fmt.Fprintf(stdout, "job %s: %s\n", report.JobID, report.Status)
		for _, output := range report.Outputs {
			fmt.Fprintf(stdout, "%s\t%d\n", output.Path, output.SizeBytes)
		}
		if report.Message != "" {
			fmt.Fprintln(stdout, report.Message)
		}
		if report.Error != nil {
			writeError(stderr, report.Error)
		}
	}

	return exitCodeForStatus(report.Status)
}

// outputEntries resolves the path and size of every output. Unreadable paths
// keep a zero size instead of failing the report.
func outputEntries(paths []string) []outputEntry {
	entries := make([]outputEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, outputEntryFor(path))
	}

	return entries
}

func outputEntryFor(path string) outputEntry {
	entry := outputEntry{Path: path}

	info, err := os.Stat(path)
	if err != nil {
		return entry
	}
	if !info.IsDir() {
		entry.SizeBytes = info.Size()
		return entry
	}

	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	entry.SizeBytes = total

	return entry
}
