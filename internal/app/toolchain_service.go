package app

import (
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/toolchain"
)

// ToolchainService exposes the managed toolchain installer.
type ToolchainService struct {
	service *toolchain.Service
	runtime *Runtime
}

// GetToolchainStatus returns the managed toolchain snapshot: component
// readiness, the selected Whisper model, the OCR languages and the current
// install state. It is safe to poll while an install runs.
func (s *ToolchainService) GetToolchainStatus() models.ToolchainStatus {
	if s.service == nil {
		return models.ToolchainStatus{Success: false, Message: "The local toolchain installer is not available."}
	}

	return s.service.Status()
}

// GetToolchainInstallState returns the same snapshot as GetToolchainStatus.
// It exists so a UI that loads after the install events fired can render the
// current phase, percent and error immediately.
func (s *ToolchainService) GetToolchainInstallState() models.ToolchainStatus {
	return s.GetToolchainStatus()
}

// StartToolchainInstall starts or repairs the managed toolchain. Invalid
// options and unsupported platforms return a status with Error instead of
// starting a background install.
func (s *ToolchainService) StartToolchainInstall(options models.ToolchainInstallOptions) models.ToolchainStatus {
	if s.service == nil {
		return models.ToolchainStatus{Success: false, Message: "The local toolchain installer is not available."}
	}

	return s.service.Start(s.runtime.ContextOrBackground(), options)
}

// CancelToolchainInstall stops the active install and cleans its staging
// files. A cancelled install is never reported as ready.
func (s *ToolchainService) CancelToolchainInstall() models.ToolchainStatus {
	if s.service == nil {
		return models.ToolchainStatus{Success: false, Message: "The local toolchain installer is not available."}
	}

	return s.service.Cancel()
}
