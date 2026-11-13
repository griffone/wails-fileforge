package pdf

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
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

func (t *MergeTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves the merge destination without writing anything. An explicit
// outputPath keeps the existing contract (it must not exist); otherwise the
// output defaults to <firstStem>_merged.pdf next to the first input.
func (t *MergeTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	if req.Mode != "single" {
		return planFailure(plan, &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: "mode must be single"})
	}

	outputPath, naturalPath, adjusted := resolveMergeOutputPath(req.InputPaths, req.OutputDir, req.Options)

	if mergeErr := engine.ValidateMergePaths(req.InputPaths, outputPath); mergeErr != nil {
		return planFailure(plan, mapMergeError(mergeErr))
	}

	planned := models.PlannedOutputV1{
		InputPath:  strings.Join(req.InputPaths, ","),
		OutputPath: outputPath,
		OutputKind: "file",
		Adjusted:   adjusted,
	}
	if adjusted {
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", filepath.Base(naturalPath), filepath.Base(outputPath)))
	}

	plan.Success = true
	plan.Message = fmt.Sprintf("Ready to merge %d PDFs.", len(req.InputPaths))
	plan.Outputs = []models.PlannedOutputV1{planned}

	return plan, nil
}

func (t *MergeTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	outputPath, _, _ := resolveMergeOutputPath(req.InputPaths, req.OutputDir, req.Options)
	inputPath := strings.Join(req.InputPaths, ",")

	err := engine.Merge(ctx, req.InputPaths, outputPath)
	if err != nil {
		jobErr := mapMergeError(err)
		return models.JobResultItemV1{
			InputPath:   inputPath,
			OutputPath:  outputPath,
			Outputs:     mergeOutputs(outputPath),
			OutputCount: mergeOutputCount(outputPath),
			Success:     false,
			Message:     jobErr.Message,
			Error:       jobErr,
		}, jobErr
	}

	return models.JobResultItemV1{
		InputPath:   inputPath,
		OutputPath:  outputPath,
		Outputs:     mergeOutputs(outputPath),
		OutputCount: mergeOutputCount(outputPath),
		Success:     true,
		Message:     "PDF merge successful",
	}, nil
}

// resolveMergeOutputPath returns the resolved output path, the natural
// (unsuffixed) auto path and whether the auto path had to be adjusted.
func resolveMergeOutputPath(inputPaths []string, outputDir string, options map[string]any) (string, string, bool) {
	if explicit := outputPathFromOptions(options); explicit != "" {
		return explicit, explicit, false
	}

	if len(inputPaths) == 0 {
		return "", "", false
	}

	first := strings.TrimSpace(inputPaths[0])
	if first == "" {
		return "", "", false
	}

	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = filepath.Dir(first)
	}
	stem := strings.TrimSuffix(filepath.Base(first), filepath.Ext(first))
	if strings.TrimSpace(stem) == "" {
		stem = "document"
	}

	base := stem + "_merged"
	naturalPath := filepath.Join(dir, base+".pdf")
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, "pdf", map[string]struct{}{})

	return outputPath, naturalPath, adjusted
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

func mergeOutputs(outputPath string) []string {
	if outputPath == "" {
		return nil
	}
	return []string{outputPath}
}

func mergeOutputCount(outputPath string) int {
	if outputPath == "" {
		return 0
	}
	return 1
}

func planFailure(plan models.PlanJobResponseV1, jobErr *models.JobErrorV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}
	return plan, jobErr
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
