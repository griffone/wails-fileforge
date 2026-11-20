package models

// ToolchainComponent is one portable component of the managed local
// toolchain. Ready means the file was verified on disk; Required components
// gate ToolchainStatus.Ready.
type ToolchainComponent struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Ready    bool   `json:"ready"`
	Required bool   `json:"required"`
	Path     string `json:"path,omitempty"`
	Version  string `json:"version,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// ToolchainStatus is the managed toolchain snapshot: component readiness,
// the selected Whisper model, the installed OCR languages and the current
// install state. It is safe to poll while an install runs.
type ToolchainStatus struct {
	Success      bool                 `json:"success"`
	Message      string               `json:"message"`
	Ready        bool                 `json:"ready"`
	Installing   bool                 `json:"installing"`
	Cancelling   bool                 `json:"cancelling,omitempty"`
	Percent      int                  `json:"percent"`
	Phase        string               `json:"phase,omitempty"`
	Detail       string               `json:"detail,omitempty"`
	Error        string               `json:"error,omitempty"`
	StorageDir   string               `json:"storageDir"`
	Components   []ToolchainComponent `json:"components"`
	Model        string               `json:"model"`
	ModelPath    string               `json:"modelPath,omitempty"`
	OCRLanguages []string             `json:"ocrLanguages"`
}

// ToolchainInstallOptions selects what the installer must provide. An empty
// model installs "small"; an empty language list installs English and Spanish.
type ToolchainInstallOptions struct {
	Model        string   `json:"model"`        // small|medium|large-v3; empty = small
	OCRLanguages []string `json:"ocrLanguages"` // eng|spa; empty = both
	Force        bool     `json:"force,omitempty"`
}

// ToolchainInstallEvent is one installer progress update emitted on
// toolchain/install/v1. Done marks a terminal event; Error marks a failure.
type ToolchainInstallEvent struct {
	Phase         string `json:"phase"`
	Percent       int    `json:"percent"`
	Indeterminate bool   `json:"indeterminate,omitempty"`
	Message       string `json:"message"`
	Done          bool   `json:"done,omitempty"`
	Error         string `json:"error,omitempty"`
}
