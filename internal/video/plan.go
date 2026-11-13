package video

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/video/engine"
)

const (
	videoOutputKindFile = "file"

	videoDefaultTargetFormat  = engine.TargetFormatMP4
	videoDefaultQualityPreset = engine.QualityPresetMedium
)

// resolveVideoOutputOptions applies the shared preset defaults. The UI can rely
// on these values being present even when the request omits them.
func resolveVideoOutputOptions(options map[string]any, validationCode string) (string, string, *models.JobErrorV1) {
	targetFormat := strings.ToLower(optionString(options, "targetFormat"))
	switch targetFormat {
	case "":
		targetFormat = videoDefaultTargetFormat
	case engine.TargetFormatMP4, engine.TargetFormatWebM:
	default:
		return "", "", &models.JobErrorV1{Code: validationCode, Message: "targetFormat must be mp4 or webm"}
	}

	qualityPreset := strings.ToLower(optionString(options, "qualityPreset"))
	switch qualityPreset {
	case "":
		qualityPreset = videoDefaultQualityPreset
	case engine.QualityPresetHigh, engine.QualityPresetMedium, engine.QualityPresetLow:
	default:
		return "", "", &models.JobErrorV1{Code: validationCode, Message: "qualityPreset must be high, medium, or low"}
	}

	return targetFormat, qualityPreset, nil
}

// planAutoVideoOutput resolves "<stem><suffix>.<format>" next to the input, or
// inside outputDir when the user chose a folder. Reserved names and files on
// disk are never reused; a suffixed name marks the plan as adjusted.
func planAutoVideoOutput(inputPath, outputDir, suffix, format string, used map[string]struct{}) models.PlannedOutputV1 {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputKind: videoOutputKindFile,
	}

	targetDir := strings.TrimSpace(outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}
	targetDir = filepath.Clean(targetDir)

	stem := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(stem) == "" {
		stem = "video"
	}

	base := stem + suffix
	naturalPath := filepath.Join(targetDir, fmt.Sprintf("%s.%s", base, format))
	outputPath, adjusted := outputplan.UniqueFilePath(targetDir, base, format, used)

	planned.OutputPath = outputPath
	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", filepath.Base(naturalPath), filepath.Base(outputPath)))
	}

	return planned
}

// reserveInputPaths prevents an automatic output from overwriting a different
// input of the same batch.
func reserveInputPaths(inputPaths []string) map[string]struct{} {
	used := make(map[string]struct{}, len(inputPaths))
	for _, inputPath := range inputPaths {
		outputplan.MarkUsed(used, inputPath)
	}
	return used
}

func resolveRuntimeProbe(probe engine.RuntimeProbe) engine.RuntimeProbe {
	if probe != nil {
		return probe
	}
	return engine.NewFFmpegRuntimeProbe()
}

func checkVideoRuntime(ctx context.Context, probe engine.RuntimeProbe) *models.JobErrorV1 {
	if runtimeErr := resolveRuntimeProbe(probe).Check(ctx); runtimeErr != nil {
		return mapVideoError(runtimeErr)
	}
	return nil
}

func firstPlannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}
	return plan.Outputs[0].OutputPath
}

func planFailure(plan models.PlanJobResponseV1, jobErr *models.JobErrorV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}
	return plan, jobErr
}

func videoPlanFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
	item := models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
	if outputPath != "" {
		item.Outputs = []string{outputPath}
		item.OutputCount = 1
	}
	return item
}
