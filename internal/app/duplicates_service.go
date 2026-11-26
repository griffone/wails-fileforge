package app

import (
	"fileforge-desktop/internal/duplicates"
	"fileforge-desktop/internal/models"
)

// DuplicatesService exposes the read-only duplicate scanner. It is not a tool:
// its inputs are folders, so it owns a scan registry instead of a job engine.
type DuplicatesService struct {
	service *duplicates.Service
	runtime *Runtime
}

// StartScan validates a request, starts a background scan and returns its id.
func (s *DuplicatesService) StartScan(request models.DuplicateScanRequest) models.DuplicateStartResponse {
	if s.service == nil {
		return models.DuplicateStartResponse{Success: false, Message: "the duplicate scanner is not available"}
	}
	// Wails cancels the bound-method context as soon as the RPC returns, so the
	// scan is parented on the application lifetime instead.
	return s.service.StartScan(s.runtime.ContextOrBackground(), request)
}

// GetScan returns one scan snapshot, including its groups and warnings.
func (s *DuplicatesService) GetScan(scanID string) models.DuplicateScanStatusResponse {
	if s.service == nil {
		return models.DuplicateScanStatusResponse{Success: false, Message: "the duplicate scanner is not available"}
	}

	return s.service.GetScan(scanID)
}

// CancelScan asks a running scan to stop. Nothing on disk is affected.
func (s *DuplicatesService) CancelScan(scanID string) models.DuplicateScanActionResponse {
	if s.service == nil {
		return models.DuplicateScanActionResponse{Success: false, Message: "the duplicate scanner is not available"}
	}

	return s.service.CancelScan(scanID)
}

// ListScans lists the retained scans, newest first.
func (s *DuplicatesService) ListScans() models.DuplicateScanListResponse {
	if s.service == nil {
		return models.DuplicateScanListResponse{Success: false, Message: "the duplicate scanner is not available", Scans: []models.DuplicateScanState{}}
	}

	return s.service.ListScans()
}
