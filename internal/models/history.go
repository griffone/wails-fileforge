package models

// JobOutputRecord records one output file produced by a terminal job. The
// size and modification time are the fingerprint used by undo: a file whose
// fingerprint no longer matches is never moved.
type JobOutputRecord struct {
	Path        string `json:"path"`
	SizeBytes   int64  `json:"sizeBytes"`
	ModifiedAt  int64  `json:"modifiedAt"`
	TrashedPath string `json:"trashedPath,omitempty"`
	Undone      bool   `json:"undone,omitempty"`
}

// JobHistoryEntry is one recorded job with its undoable outputs.
type JobHistoryEntry struct {
	JobID        string            `json:"jobId"`
	ToolID       string            `json:"toolId"`
	CreatedAt    int64             `json:"createdAt"`
	Undone       bool              `json:"undone,omitempty"`
	TrashEmptied bool              `json:"trashEmptied,omitempty"`
	Outputs      []JobOutputRecord `json:"outputs"`
}

// UndoSkipped reports one output that undo or restore refused to touch.
type UndoSkipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// UndoResult reports the per-file outcome of an undo or restore request.
type UndoResult struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Moved   []string      `json:"moved"`
	Skipped []UndoSkipped `json:"skipped"`
}
