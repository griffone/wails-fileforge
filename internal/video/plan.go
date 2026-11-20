package video

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
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
func resolveVideoOutputOptions(options map[string]any, validationCode string) (string, string, *models.JobError) {
	targetFormat := strings.ToLower(optionString(options, "targetFormat"))
	switch targetFormat {
	case "":
		targetFormat = videoDefaultTargetFormat
	case engine.TargetFormatMP4, engine.TargetFormatWebM:
	default:
		return "", "", &models.JobError{Code: validationCode, Message: "targetFormat must be mp4 or webm"}
	}

	qualityPreset := strings.ToLower(optionString(options, "qualityPreset"))
	switch qualityPreset {
	case "":
		qualityPreset = videoDefaultQualityPreset
	case engine.QualityPresetHigh, engine.QualityPresetMedium, engine.QualityPresetLow:
	default:
		return "", "", &models.JobError{Code: validationCode, Message: "qualityPreset must be high, medium, or low"}
	}

	return targetFormat, qualityPreset, nil
}

// planAutoVideoOutput resolves "<stem><suffix>.<format>" next to the input, or
// inside outputDir when the user chose a folder. Reserved names and files on
// disk are never reused; a suffixed name marks the plan as adjusted.
func planAutoVideoOutput(inputPath, outputDir, suffix, format string, used map[string]struct{}) models.PlannedOutput {
	planned := models.PlannedOutput{
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

// videoOptionInt reads an optional integer option and returns 0 when the option
// is absent. Supplied values of the wrong type are rejected so a typo never
// silently changes the output.
func videoOptionInt(options map[string]any, key, validationCode string) (int, *models.JobError) {
	if !videoOptionSupplied(options, key) {
		return 0, nil
	}

	if floatValue, isFloat := options[key].(float64); isFloat && floatValue != float64(int(floatValue)) {
		return 0, &models.JobError{Code: validationCode, Message: fmt.Sprintf("options.%s must be an integer", key)}
	}

	switch value := options[key].(type) {
	case float64:
		return int(value), nil
	case float32:
		return int(value), nil
	case int:
		return value, nil
	case int32:
		return int(value), nil
	case int64:
		return int(value), nil
	default:
		return 0, &models.JobError{Code: validationCode, Message: fmt.Sprintf("options.%s must be numeric", key)}
	}
}

// videoOptionFloat reads an optional numeric option. Supplied values of the
// wrong type are rejected. The boolean reports whether the option was supplied.
func videoOptionFloat(options map[string]any, key, validationCode string) (float64, bool, *models.JobError) {
	if !videoOptionSupplied(options, key) {
		return 0, false, nil
	}

	switch value := options[key].(type) {
	case float64:
		return value, true, nil
	case float32:
		return float64(value), true, nil
	case int:
		return float64(value), true, nil
	case int32:
		return float64(value), true, nil
	case int64:
		return float64(value), true, nil
	default:
		return 0, true, &models.JobError{Code: validationCode, Message: fmt.Sprintf("options.%s must be numeric", key)}
	}
}

// videoOptionBool reads an optional boolean option and returns false when the
// option is absent. Textual values are accepted because some callers serialize
// checkboxes as strings.
func videoOptionBool(options map[string]any, key, validationCode string) (bool, *models.JobError) {
	if !videoOptionSupplied(options, key) {
		return false, nil
	}

	switch value := options[key].(type) {
	case bool:
		return value, nil
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err == nil {
			return parsed, nil
		}
	}

	return false, &models.JobError{Code: validationCode, Message: fmt.Sprintf("options.%s must be a boolean", key)}
}

// videoOptionSupplied reports whether a request option was provided with a value.
func videoOptionSupplied(options map[string]any, key string) bool {
	if options == nil {
		return false
	}

	value, ok := options[key]
	return ok && value != nil
}

func resolveRuntimeProbe(probe engine.RuntimeProbe) engine.RuntimeProbe {
	if probe != nil {
		return probe
	}
	return engine.NewFFmpegRuntimeProbe()
}

func checkVideoRuntime(ctx context.Context, probe engine.RuntimeProbe) *models.JobError {
	if runtimeErr := resolveRuntimeProbe(probe).Check(ctx); runtimeErr != nil {
		return mapVideoError(runtimeErr)
	}
	return nil
}

func firstPlannedOutputPath(plan models.PlanJobResponse) string {
	if len(plan.Outputs) == 0 {
		return ""
	}
	return plan.Outputs[0].OutputPath
}

func planFailure(plan models.PlanJobResponse, jobErr *models.JobError) (models.PlanJobResponse, *models.JobError) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}
	return plan, jobErr
}

func videoPlanFailureItem(inputPath, outputPath string, jobErr *models.JobError) models.JobResultItem {
	item := models.JobResultItem{
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
