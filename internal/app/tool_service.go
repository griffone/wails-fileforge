package app

import (
	"strings"

	"fileforge-desktop/internal/image"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/services"
)

// ToolService exposes the tool catalog, the job lifecycle, the undo history,
// and the image preview endpoints used by the crop and annotate editors.
type ToolService struct {
	tooling  *services.ToolingService
	registry *registry.Registry
	runtime  *Runtime
}

func (s *ToolService) ListTools() models.ListToolsResponse {
	return s.tooling.ListTools()
}

func (s *ToolService) ValidateJob(req models.JobRequest) models.ValidateJobResponse {
	return s.tooling.ValidateJob(req)
}

func (s *ToolService) PlanJob(req models.JobRequest) models.PlanJobResponse {
	return s.tooling.PlanJob(req)
}

func (s *ToolService) RunJob(req models.JobRequest) models.RunJobResponse {
	return s.tooling.RunJob(req)
}

func (s *ToolService) CancelJob(jobID string) models.CancelJobResponse {
	return s.tooling.CancelJob(jobID)
}

func (s *ToolService) GetJobStatus(jobID string) models.JobStatusResponse {
	return s.tooling.GetJobStatus(jobID)
}

// ListJobs returns every tracked tool job, newest first. The UI uses it to
// rebuild the background activity list without polling each job.
func (s *ToolService) ListJobs(limit int) models.JobListResponse {
	return s.tooling.ListJobs(limit)
}

// VerifyJob verifies every output of a finished job: existence, readability,
// size, file type and media-specific metadata. The report lists passing and
// failing checks per output.
func (s *ToolService) VerifyJob(jobID string) models.VerificationReport {
	return s.tooling.VerifyJob(jobID)
}

// ListJobHistory returns recorded terminal jobs with their output
// fingerprints, newest first. The history survives app restarts.
func (s *ToolService) ListJobHistory() []models.JobHistoryEntry {
	return s.tooling.ListJobHistory()
}

// UndoJob moves the untouched outputs of a recorded job to the app trash.
// Modified or missing outputs are skipped and reported per file.
func (s *ToolService) UndoJob(jobID string) models.UndoResult {
	return s.tooling.UndoJob(jobID)
}

// RestoreJob moves trashed outputs back to their original paths when the
// destination is free.
func (s *ToolService) RestoreJob(jobID string) models.UndoResult {
	return s.tooling.RestoreJob(jobID)
}

// EmptyTrash permanently removes the app-managed trash. User files outside
// the trash are never touched.
func (s *ToolService) EmptyTrash() models.PathActionResponse {
	return s.tooling.EmptyTrash()
}

func (s *ToolService) GetImagePreviewSource(inputPath string) models.ImagePreviewSourceResponse {
	tool, ok := s.imageCropTool()
	if !ok {
		return models.ImagePreviewSourceResponse{
			Success: false,
			Message: "Image crop tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.crop is not registered", nil),
		}
	}

	return tool.GetImagePreviewSource(strings.TrimSpace(inputPath))
}

func (s *ToolService) GetImageCropPreview(req models.ImageCropPreviewRequest) models.ImageCropPreviewResponse {
	tool, ok := s.imageCropTool()
	if !ok {
		return models.ImageCropPreviewResponse{
			Success: false,
			Message: "Image crop tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.crop is not registered", nil),
		}
	}

	return tool.GetImageCropPreview(req)
}

func (s *ToolService) GetImageAnnotatePreview(req models.ImageAnnotatePreviewRequest) models.ImageAnnotatePreviewResponse {
	tool, ok := s.imageAnnotateTool()
	if !ok {
		return models.ImageAnnotatePreviewResponse{
			Success: false,
			Message: "Image annotate tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.annotate is not registered", nil),
		}
	}

	return tool.GetImageAnnotatePreview(req)
}

func (s *ToolService) imageCropTool() (*image.CropTool, bool) {
	if s.registry == nil {
		return nil, false
	}

	rawTool, err := s.registry.GetTool(image.ToolIDImageCrop)
	if err != nil {
		return nil, false
	}

	cropTool, ok := rawTool.(*image.CropTool)
	if !ok {
		return nil, false
	}

	return cropTool, true
}

func (s *ToolService) imageAnnotateTool() (*image.AnnotateTool, bool) {
	if s.registry == nil {
		return nil, false
	}

	rawTool, err := s.registry.GetTool(image.ToolIDImageAnnotate)
	if err != nil {
		return nil, false
	}

	annotateTool, ok := rawTool.(*image.AnnotateTool)
	if !ok {
		return nil, false
	}

	return annotateTool, true
}
