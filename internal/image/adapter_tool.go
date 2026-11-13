package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
)

const (
	ToolIDImageConvertV1 = "tool.image.convert"

	imageDefaultFormat  = "webp"
	imageOutputKindFile = "file"
)

var imageInputExtensions = []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif"}

type ImageToolAdapter struct {
	converter *ImageConverter
}

func NewImageToolAdapter(converter *ImageConverter) *ImageToolAdapter {
	if converter == nil {
		converter = NewImageConverter()
	}

	return &ImageToolAdapter{converter: converter}
}

func (t *ImageToolAdapter) ID() string {
	return ToolIDImageConvertV1
}

func (t *ImageToolAdapter) Capability() string {
	return "tool.image.convert"
}

func (t *ImageToolAdapter) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Image Converter",
		Description:      "Image conversion tool exposed as Tool V1 jobs",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  append([]string(nil), imageInputExtensions...),
		OutputExtensions: t.converter.SupportedFormats(),
		RuntimeDeps:      []string{"libvips"},
		Tags:             []string{"image", "convert"},
	}
}

func (t *ImageToolAdapter) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *ImageToolAdapter) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves every output path for the request without converting anything.
func (t *ImageToolAdapter) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	if len(req.InputPaths) == 0 {
		return planFailure(plan, models.NewCanonicalJobError("IMAGE_INPUT_REQUIRED", "Choose at least one image to convert.", nil))
	}

	if req.Mode != "single" && req.Mode != "batch" {
		return planFailure(plan, models.NewCanonicalJobError("IMAGE_MODE_INVALID", "mode must be single or batch", nil))
	}

	if req.Mode == "single" && len(req.InputPaths) != 1 {
		return planFailure(plan, models.NewCanonicalJobError("IMAGE_SINGLE_INPUT_COUNT", "single mode requires exactly one input path", nil))
	}

	format := t.resolveFormat(req.Options)
	if err := t.converter.validateOutputFormat(format); err != nil {
		return planFailure(plan, models.NewCanonicalJobError("IMAGE_FORMAT_INVALID", err.Error(), nil))
	}

	outputDir, explicitOutput := t.planDestinations(req)

	// Reserve every input path so an automatically derived output can never
	// overwrite a different input of the same batch.
	used := make(map[string]struct{}, len(req.InputPaths))
	for _, inputPath := range req.InputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	outputs := make([]models.PlannedOutputV1, 0, len(req.InputPaths))
	warnings := make([]string, 0)
	for index, inputPath := range req.InputPaths {
		allowExplicit := req.Mode == "single" && index == 0
		planned, planErr := t.planOutput(inputPath, outputDir, explicitOutput, format, allowExplicit, used)
		if planErr != nil {
			if planned.OutputPath != "" {
				outputs = append(outputs, planned)
				plan.Outputs = outputs
			}
			return planFailure(plan, planErr)
		}
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = imagePlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = t.effectiveOptions(req.Options, format)

	return plan, nil
}

func (t *ImageToolAdapter) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := firstInputPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imageFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	inputPath = req.InputPaths[0]
	outputPath := plan.Outputs[0].OutputPath
	format := t.resolveFormat(req.Options)

	if err := t.converter.ConvertSingle(ctx, inputPath, outputPath, format, req.Options); err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_SINGLE_EXECUTION", err.Error(), nil)
		return imageFailureItem(inputPath, outputPath, jobErr), jobErr
	}

	return models.JobResultItemV1{
		InputPath:   inputPath,
		OutputPath:  outputPath,
		Outputs:     []string{outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "conversion successful",
	}, nil
}

func (t *ImageToolAdapter) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	if len(req.InputPaths) == 0 {
		return nil, models.NewCanonicalJobError("IMAGE_INPUT_REQUIRED", "Choose at least one image to convert.", nil)
	}

	format := t.resolveFormat(req.Options)
	if err := t.converter.validateOutputFormat(format); err != nil {
		return nil, models.NewCanonicalJobError("IMAGE_FORMAT_INVALID", err.Error(), nil)
	}

	outputDir, _ := t.planDestinations(req)
	used := make(map[string]struct{}, len(req.InputPaths))
	for _, inputPath := range req.InputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	items := make([]models.JobResultItemV1, 0, len(req.InputPaths))
	total := len(req.InputPaths)
	for index, inputPath := range req.InputPaths {
		select {
		case <-ctx.Done():
			return items, models.NewCanonicalJobError("IMAGE_BATCH_CANCELLED", ctx.Err().Error(), nil)
		default:
		}

		items = append(items, t.executePlannedItem(ctx, inputPath, outputDir, format, used, req.Options))

		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: index + 1,
				Total:   total,
				Stage:   models.JobStatusRunning,
				Message: fmt.Sprintf("processed %d/%d", index+1, total),
			})
		}
	}

	var firstErr *models.JobErrorV1
	for _, item := range items {
		if !item.Success && item.Error != nil {
			firstErr = item.Error
			break
		}
	}

	return items, firstErr
}

func (t *ImageToolAdapter) executePlannedItem(ctx context.Context, inputPath, outputDir, format string, used map[string]struct{}, options map[string]any) models.JobResultItemV1 {
	planned, planErr := t.planOutput(inputPath, outputDir, "", format, false, used)
	if planErr != nil {
		return imageFailureItem(inputPath, planned.OutputPath, planErr)
	}

	if err := t.converter.ConvertSingle(ctx, inputPath, planned.OutputPath, format, options); err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_BATCH_ITEM", err.Error(), map[string]any{"inputPath": inputPath})
		return imageFailureItem(inputPath, planned.OutputPath, jobErr)
	}

	return models.JobResultItemV1{
		InputPath:   inputPath,
		OutputPath:  planned.OutputPath,
		Outputs:     []string{planned.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "conversion successful",
	}
}

// planDestinations returns the fallback output directory and the explicit
// output path from the request options.
func (t *ImageToolAdapter) planDestinations(req models.JobRequestV1) (string, string) {
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = convertOptionString(req.Options, "outputDir")
	}

	explicitOutput := ""
	if req.Mode == "single" {
		explicitOutput = convertOptionString(req.Options, "outputPath")
	}

	return outputDir, explicitOutput
}

// planOutput resolves one input into one planned output file. It never writes.
func (t *ImageToolAdapter) planOutput(inputPath, outputDir, explicitOutput, format string, allowExplicit bool, used map[string]struct{}) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputKind: imageOutputKindFile,
	}

	input := strings.TrimSpace(inputPath)
	if input == "" {
		return planned, models.NewCanonicalJobError("IMAGE_INPUT_NOT_FOUND", "Choose an image to convert.", nil)
	}

	info, err := os.Stat(input)
	if err != nil {
		return planned, imageInputAccessError(input, err)
	}

	if info.IsDir() {
		return planned, models.NewCanonicalJobError(
			"IMAGE_INPUT_UNSUPPORTED",
			fmt.Sprintf("%s is a folder, not an image.", filepath.Base(input)),
			map[string]any{"inputPath": input},
		)
	}

	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(input), "."))
	if !t.supportsInputExtension(extension) {
		return planned, models.NewCanonicalJobError(
			"IMAGE_INPUT_UNSUPPORTED",
			fmt.Sprintf("%s is not a supported image. Use %s.", filepath.Base(input), strings.Join(imageInputExtensions, ", ")),
			map[string]any{"inputPath": input, "extension": extension},
		)
	}

	if allowExplicit && explicitOutput != "" {
		return t.planExplicitOutput(input, explicitOutput, planned)
	}

	targetDir := strings.TrimSpace(outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(input)
	}
	targetDir = filepath.Clean(targetDir)

	if dirErr := ensureOutputDirectory(targetDir); dirErr != nil {
		return planned, dirErr
	}

	stem := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	if strings.TrimSpace(stem) == "" {
		stem = "image"
	}

	base := stem
	sameAsInput := sameFilePath(filepath.Join(targetDir, fmt.Sprintf("%s.%s", stem, format)), input)
	if sameAsInput {
		base = stem + "-converted"
	}

	outputPath, adjusted := outputplan.UniqueFilePath(targetDir, base, format, used)
	planned.OutputPath = outputPath

	if sameAsInput {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("Saving as %s to keep the original image.", filepath.Base(outputPath)))
	} else if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", fmt.Sprintf("%s.%s", stem, format), filepath.Base(outputPath)))
	}

	return planned, nil
}

func (t *ImageToolAdapter) planExplicitOutput(inputPath, explicitOutput string, planned models.PlannedOutputV1) (models.PlannedOutputV1, *models.JobErrorV1) {
	outputPath := filepath.Clean(strings.TrimSpace(explicitOutput))
	planned.OutputPath = outputPath

	if sameFilePath(inputPath, outputPath) {
		return planned, models.NewCanonicalJobError(
			"IMAGE_OUTPUT_SAME_AS_INPUT",
			fmt.Sprintf("%s is the input file. Choose a different name.", filepath.Base(outputPath)),
			map[string]any{"outputPath": outputPath},
		)
	}

	if info, err := os.Stat(outputPath); err == nil {
		planned.Exists = true
		if info.IsDir() {
			return planned, models.NewCanonicalJobError(
				"IMAGE_OUTPUT_DIR_INVALID",
				fmt.Sprintf("%s is a folder, not a file.", filepath.Base(outputPath)),
				map[string]any{"outputPath": outputPath},
			)
		}
		return planned, models.NewCanonicalJobError(
			"IMAGE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(outputPath)),
			map[string]any{"outputPath": outputPath},
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return planned, models.NewCanonicalJobError(
			"IMAGE_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s cannot be accessed. Choose another location.", filepath.Base(outputPath)),
			map[string]any{"outputPath": outputPath},
		)
	}

	if dirErr := ensureOutputDirectory(filepath.Dir(outputPath)); dirErr != nil {
		return planned, dirErr
	}

	return planned, nil
}

func (t *ImageToolAdapter) resolveFormat(options map[string]any) string {
	format := convertOptionString(options, "format")
	if format == "" {
		return imageDefaultFormat
	}
	return strings.ToLower(format)
}

func (t *ImageToolAdapter) supportsInputExtension(extension string) bool {
	normalized := strings.ToLower(strings.TrimSpace(extension))
	for _, supported := range imageInputExtensions {
		if supported == normalized {
			return true
		}
	}
	return false
}

func (t *ImageToolAdapter) effectiveOptions(options map[string]any, format string) map[string]any {
	effective := map[string]any{"format": format}
	for _, key := range []string{"quality", "width", "height"} {
		if value, ok := numericOption(options, key); ok {
			effective[key] = value
		}
	}
	return effective
}

func planFailure(plan models.PlanJobResponseV1, jobErr *models.JobErrorV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}
	return plan, jobErr
}

func imagePlanMessage(count int) string {
	if count == 1 {
		return "Ready to convert 1 image."
	}
	return fmt.Sprintf("Ready to convert %d images.", count)
}

func imageInputAccessError(inputPath string, err error) *models.JobErrorV1 {
	if errors.Is(err, os.ErrNotExist) {
		return models.NewCanonicalJobError(
			"IMAGE_INPUT_NOT_FOUND",
			fmt.Sprintf("%s was not found.", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	return models.NewCanonicalJobError(
		"IMAGE_INPUT_NOT_FOUND",
		fmt.Sprintf("%s cannot be read.", filepath.Base(inputPath)),
		map[string]any{"inputPath": inputPath},
	)
}

func ensureOutputDirectory(dir string) *models.JobErrorV1 {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return models.NewCanonicalJobError(
				"IMAGE_OUTPUT_DIR_INVALID",
				fmt.Sprintf("The output folder does not exist: %s", dir),
				map[string]any{"outputDir": dir},
			)
		}
		return models.NewCanonicalJobError(
			"IMAGE_OUTPUT_DIR_INVALID",
			fmt.Sprintf("The output folder cannot be used: %s", dir),
			map[string]any{"outputDir": dir},
		)
	}

	if !info.IsDir() {
		return models.NewCanonicalJobError(
			"IMAGE_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s is not a folder.", dir),
			map[string]any{"outputDir": dir},
		)
	}

	return nil
}

func imageFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
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

func plannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}
	return plan.Outputs[0].OutputPath
}

func firstInputPath(inputPaths []string) string {
	if len(inputPaths) == 0 {
		return ""
	}
	return inputPaths[0]
}

func convertOptionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}

	value, ok := options[key].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}

func numericOption(options map[string]any, key string) (int, bool) {
	if options == nil {
		return 0, false
	}

	switch value := options[key].(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float32:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

func sameFilePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftClean := filepath.Clean(left)
	rightClean := filepath.Clean(right)
	if leftClean == rightClean {
		return true
	}
	if abs, err := filepath.Abs(leftClean); err == nil {
		leftClean = abs
	}
	if abs, err := filepath.Abs(rightClean); err == nil {
		rightClean = abs
	}
	if leftClean == rightClean {
		return true
	}
	if eval, err := filepath.EvalSymlinks(leftClean); err == nil {
		leftClean = eval
	}
	if eval, err := filepath.EvalSymlinks(rightClean); err == nil {
		rightClean = eval
	}
	return leftClean == rightClean
}
