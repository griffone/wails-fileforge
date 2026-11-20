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

// GetToolchainStatusV1 returns the managed toolchain snapshot: component
// readiness, the selected Whisper model, the OCR languages and the current
// install state. It is safe to poll while an install runs.
func (s *ToolchainService) GetToolchainStatusV1() models.ToolchainStatusV1 {
	if s.service == nil {
		return models.ToolchainStatusV1{Success: false, Message: "The local toolchain installer is not available."}
	}

	return s.service.Status()
}

// GetToolchainInstallStateV1 returns the same snapshot as GetToolchainStatusV1.
// It exists so a UI that loads after the install events fired can render the
// current phase, percent and error immediately.
func (s *ToolchainService) GetToolchainInstallStateV1() models.ToolchainStatusV1 {
	return s.GetToolchainStatusV1()
}

// StartToolchainInstallV1 starts or repairs the managed toolchain. Invalid
// options and unsupported platforms return a status with Error instead of
// starting a background install.
func (s *ToolchainService) StartToolchainInstallV1(options models.ToolchainInstallOptionsV1) models.ToolchainStatusV1 {
	if s.service == nil {
		return models.ToolchainStatusV1{Success: false, Message: "The local toolchain installer is not available."}
	}

	return s.service.Start(s.runtime.ContextOrBackground(), options)
}

// CancelToolchainInstallV1 stops the active install and cleans its staging
// files. A cancelled install is never reported as ready.
func (s *ToolchainService) CancelToolchainInstallV1() models.ToolchainStatusV1 {
	if s.service == nil {
		return models.ToolchainStatusV1{Success: false, Message: "The local toolchain installer is not available."}
	}

	return s.service.Cancel()
}
