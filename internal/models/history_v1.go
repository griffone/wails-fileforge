package models

// JobOutputRecordV1 records one output file produced by a terminal job. The
// size and modification time are the fingerprint used by undo: a file whose
// fingerprint no longer matches is never moved.
type JobOutputRecordV1 struct {
	Path        string `json:"path"`
	SizeBytes   int64  `json:"sizeBytes"`
	ModifiedAt  int64  `json:"modifiedAt"`
	TrashedPath string `json:"trashedPath,omitempty"`
	Undone      bool   `json:"undone,omitempty"`
}

// JobHistoryEntryV1 is one recorded job with its undoable outputs.
type JobHistoryEntryV1 struct {
	JobID        string              `json:"jobId"`
	ToolID       string              `json:"toolId"`
	CreatedAt    int64               `json:"createdAt"`
	Undone       bool                `json:"undone,omitempty"`
	TrashEmptied bool                `json:"trashEmptied,omitempty"`
	Outputs      []JobOutputRecordV1 `json:"outputs"`
}

// UndoSkippedV1 reports one output that undo or restore refused to touch.
type UndoSkippedV1 struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// UndoResultV1 reports the per-file outcome of an undo or restore request.
type UndoResultV1 struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Moved   []string        `json:"moved"`
	Skipped []UndoSkippedV1 `json:"skipped"`
}
