package models

// ToolchainComponentV1 is one portable component of the managed local
// toolchain. Ready means the file was verified on disk; Required components
// gate ToolchainStatusV1.Ready.
type ToolchainComponentV1 struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Ready    bool   `json:"ready"`
	Required bool   `json:"required"`
	Path     string `json:"path,omitempty"`
	Version  string `json:"version,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// ToolchainStatusV1 is the managed toolchain snapshot: component readiness,
// the selected Whisper model, the installed OCR languages and the current
// install state. It is safe to poll while an install runs.
type ToolchainStatusV1 struct {
	Success      bool                   `json:"success"`
	Message      string                 `json:"message"`
	Ready        bool                   `json:"ready"`
	Installing   bool                   `json:"installing"`
	Cancelling   bool                   `json:"cancelling,omitempty"`
	Percent      int                    `json:"percent"`
	Phase        string                 `json:"phase,omitempty"`
	Detail       string                 `json:"detail,omitempty"`
	Error        string                 `json:"error,omitempty"`
	StorageDir   string                 `json:"storageDir"`
	Components   []ToolchainComponentV1 `json:"components"`
	Model        string                 `json:"model"`
	ModelPath    string                 `json:"modelPath,omitempty"`
	OCRLanguages []string               `json:"ocrLanguages"`
}

// ToolchainInstallOptionsV1 selects what the installer must provide. An empty
// model installs "small"; an empty language list installs English and Spanish.
type ToolchainInstallOptionsV1 struct {
	Model        string   `json:"model"`        // small|medium|large-v3; empty = small
	OCRLanguages []string `json:"ocrLanguages"` // eng|spa; empty = both
	Force        bool     `json:"force,omitempty"`
}

// ToolchainInstallEventV1 is one installer progress update emitted on
// toolchain/install/v1. Done marks a terminal event; Error marks a failure.
type ToolchainInstallEventV1 struct {
	Phase         string `json:"phase"`
	Percent       int    `json:"percent"`
	Indeterminate bool   `json:"indeterminate,omitempty"`
	Message       string `json:"message"`
	Done          bool   `json:"done,omitempty"`
	Error         string `json:"error,omitempty"`
}
