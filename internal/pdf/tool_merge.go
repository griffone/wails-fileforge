package pdf

import (
	"context"
	"errors"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

const ToolIDPDFMergeV1 = "tool.pdf.merge"

type MergeTool struct{}

func NewMergeTool() *MergeTool {
	return &MergeTool{}
}

func (t *MergeTool) ID() string {
	return ToolIDPDFMergeV1
}

func (t *MergeTool) Capability() string {
	return "tool.pdf.merge"
}

func (t *MergeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "PDF Merge",
		Description:      "Merge multiple PDF files into a single output PDF",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    false,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "merge", "documents"},
	}
}

func (t *MergeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *MergeTool) Validate(_ context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	if req.Mode != "single" {
		return &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: "mode must be single"}
	}

	if mergeErr := engine.ValidateMergePaths(req.InputPaths, outputPathFromOptions(req.Options)); mergeErr != nil {
		return mapMergeError(mergeErr)
	}

	return nil
}

func (t *MergeTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	outputPath := outputPathFromOptions(req.Options)
	err := engine.Merge(ctx, req.InputPaths, outputPath)
	if err != nil {
		jobErr := mapMergeError(err)
		return models.JobResultItemV1{
			InputPath:  strings.Join(req.InputPaths, ","),
			OutputPath: outputPath,
			Success:    false,
			Message:    jobErr.Message,
			Error:      jobErr,
		}, jobErr
	}

	return models.JobResultItemV1{
		InputPath:  strings.Join(req.InputPaths, ","),
		OutputPath: outputPath,
		Success:    true,
		Message:    "PDF merge successful",
	}, nil
}

func outputPathFromOptions(options map[string]any) string {
	if options == nil {
		return ""
	}

	outputPath, ok := options["outputPath"].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(outputPath)
}

func mapMergeError(err error) *models.JobErrorV1 {
	var mergeErr *engine.MergeError
	if !errors.As(err, &mergeErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return &models.JobErrorV1{
		Code:       mergeErr.Code,
		DetailCode: mergeErr.Code,
		Message:    mergeErr.Message,
		Details:    mergeErr.Details,
	}
}
