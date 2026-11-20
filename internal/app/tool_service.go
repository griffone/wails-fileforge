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

func (s *ToolService) ListToolsV1() models.ListToolsResponseV1 {
	return s.tooling.ListToolsV1()
}

func (s *ToolService) ValidateJobV1(req models.JobRequestV1) models.ValidateJobResponseV1 {
	return s.tooling.ValidateJobV1(req)
}

func (s *ToolService) PlanJobV1(req models.JobRequestV1) models.PlanJobResponseV1 {
	return s.tooling.PlanJobV1(req)
}

func (s *ToolService) RunJobV1(req models.JobRequestV1) models.RunJobResponseV1 {
	return s.tooling.RunJobV1(req)
}

func (s *ToolService) CancelJobV1(jobID string) models.CancelJobResponseV1 {
	return s.tooling.CancelJobV1(jobID)
}

func (s *ToolService) GetJobStatusV1(jobID string) models.JobStatusResponseV1 {
	return s.tooling.GetJobStatusV1(jobID)
}

// ListJobsV1 returns every tracked tool job, newest first. The UI uses it to
// rebuild the background activity list without polling each job.
func (s *ToolService) ListJobsV1(limit int) models.JobListResponseV1 {
	return s.tooling.ListJobsV1(limit)
}

// VerifyJobV1 verifies every output of a finished job: existence, readability,
// size, file type and media-specific metadata. The report lists passing and
// failing checks per output.
func (s *ToolService) VerifyJobV1(jobID string) models.VerificationReportV1 {
	return s.tooling.VerifyJobV1(jobID)
}

// ListJobHistoryV1 returns recorded terminal jobs with their output
// fingerprints, newest first. The history survives app restarts.
func (s *ToolService) ListJobHistoryV1() []models.JobHistoryEntryV1 {
	return s.tooling.ListJobHistoryV1()
}

// UndoJobV1 moves the untouched outputs of a recorded job to the app trash.
// Modified or missing outputs are skipped and reported per file.
func (s *ToolService) UndoJobV1(jobID string) models.UndoResultV1 {
	return s.tooling.UndoJobV1(jobID)
}

// RestoreJobV1 moves trashed outputs back to their original paths when the
// destination is free.
func (s *ToolService) RestoreJobV1(jobID string) models.UndoResultV1 {
	return s.tooling.RestoreJobV1(jobID)
}

// EmptyTrashV1 permanently removes the app-managed trash. User files outside
// the trash are never touched.
func (s *ToolService) EmptyTrashV1() models.PathActionResponseV1 {
	return s.tooling.EmptyTrashV1()
}

func (s *ToolService) GetImagePreviewSourceV1(inputPath string) models.ImagePreviewSourceResponseV1 {
	tool, ok := s.imageCropTool()
	if !ok {
		return models.ImagePreviewSourceResponseV1{
			Success: false,
			Message: "Image crop tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.crop is not registered", nil),
		}
	}

	return tool.GetImagePreviewSource(strings.TrimSpace(inputPath))
}

func (s *ToolService) GetImageCropPreviewV1(req models.ImageCropPreviewRequestV1) models.ImageCropPreviewResponseV1 {
	tool, ok := s.imageCropTool()
	if !ok {
		return models.ImageCropPreviewResponseV1{
			Success: false,
			Message: "Image crop tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.crop is not registered", nil),
		}
	}

	return tool.GetImageCropPreview(req)
}

func (s *ToolService) GetImageAnnotatePreviewV1(req models.ImageAnnotatePreviewRequestV1) models.ImageAnnotatePreviewResponseV1 {
	tool, ok := s.imageAnnotateTool()
	if !ok {
		return models.ImageAnnotatePreviewResponseV1{
			Success: false,
			Message: "Image annotate tool is unavailable.",
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", "tool.image.annotate is not registered", nil),
		}
	}

	return tool.GetImageAnnotatePreviewV1(req)
}

func (s *ToolService) imageCropTool() (*image.CropTool, bool) {
	if s.registry == nil {
		return nil, false
	}

	rawTool, err := s.registry.GetToolV2(image.ToolIDImageCropV1)
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

	rawTool, err := s.registry.GetToolV2(image.ToolIDImageAnnotateV1)
	if err != nil {
		return nil, false
	}

	annotateTool, ok := rawTool.(*image.AnnotateTool)
	if !ok {
		return nil, false
	}

	return annotateTool, true
}
