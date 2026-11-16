package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
)

const (
	ToolIDPDFOptimizeV1 = "tool.pdf.optimize"

	optimizeOutputKindFile = "file"
	optimizeOutputSuffix   = "_optimized"
)

type OptimizeTool struct{}

func NewOptimizeTool() *OptimizeTool {
	return &OptimizeTool{}
}

func (t *OptimizeTool) ID() string {
	return ToolIDPDFOptimizeV1
}

func (t *OptimizeTool) Capability() string {
	return ToolIDPDFOptimizeV1
}

func (t *OptimizeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "PDF Optimize",
		Description:      "Compress PDFs and edit document metadata",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "optimize", "compress", "metadata"},
	}
}

func (t *OptimizeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *OptimizeTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_optimized.pdf destination per input
// without writing anything. An explicit options.outputPath is only accepted for
// single mode; it must be a non-existing .pdf in a writable directory.
func (t *OptimizeTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	parsed, parseErr := optimizeReqFields(req)
	if parseErr != nil {
		return planFailure(plan, parseErr)
	}

	for _, inputPath := range parsed.InputPaths {
		if optimizeErr := engine.ValidateOptimizeInput(inputPath); optimizeErr != nil {
			return planFailure(plan, mapOptimizeError(optimizeErr))
		}
	}

	if parsed.OutputPath != "" {
		planned, planErr := planExplicitOptimizeOutput(parsed)
		if planErr != nil {
			if planned.OutputPath != "" {
				plan.Outputs = []models.PlannedOutputV1{planned}
			}
			return planFailure(plan, planErr)
		}

		plan.Success = true
		plan.Message = optimizePlanMessage(1)
		plan.Outputs = []models.PlannedOutputV1{planned}
		plan.Warnings = append(planned.Warnings, optimizeTargetWarnings(parsed)...)
		plan.EffectiveOptions = optimizeEffectiveOptions(parsed)

		return plan, nil
	}

	if parsed.OutputDir != "" {
		if optimizeErr := engine.ValidateOptimizeDestination(parsed.OutputDir); optimizeErr != nil {
			return planFailure(plan, mapOptimizeError(optimizeErr))
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.InputPaths))
	for _, inputPath := range parsed.InputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	outputs := make([]models.PlannedOutputV1, 0, len(parsed.InputPaths))
	warnings := make([]string, 0)
	for _, inputPath := range parsed.InputPaths {
		targetDir := parsed.OutputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}

		planned := planAutoOptimizeOutput(inputPath, filepath.Clean(targetDir), used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = optimizePlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = append(warnings, optimizeTargetWarnings(parsed)...)
	plan.EffectiveOptions = optimizeEffectiveOptions(parsed)

	return plan, nil
}

func (t *OptimizeTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := optimizeReqFields(req)
	if parseErr != nil {
		return optimizeFailureItem(firstInputPath(req.InputPaths), "", parseErr), parseErr
	}

	if parsed.Mode != "single" {
		jobErr := optimizeRequestError(engine.ErrorCodeValidation, "mode must be single")
		return optimizeFailureItem(parsed.InputPath, "", jobErr), jobErr
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return optimizeFailureItem(parsed.InputPath, optimizePlannedOutputPath(plan), planErr), planErr
	}

	planned := plan.Outputs[0]
	outcome, err := engine.OptimizeWithOptions(ctx, planned.InputPath, planned.OutputPath, optimizeEngineOptions(parsed))
	if err != nil {
		jobErr := mapOptimizeError(err)
		return optimizeFailureItem(planned.InputPath, planned.OutputPath, jobErr), jobErr
	}

	return models.JobResultItemV1{
		InputPath:   planned.InputPath,
		OutputPath:  planned.OutputPath,
		Outputs:     []string{planned.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     optimizeSuccessMessage(parsed, outcome),
	}, nil
}

func (t *OptimizeTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := optimizeReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	if parsed.Mode != "batch" {
		return nil, optimizeRequestError(engine.ErrorCodeValidation, "mode must be batch")
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	fileErrors := make([]map[string]any, 0)
	items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
	for i, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			return items, &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
		default:
		}

		item := models.JobResultItemV1{
			InputPath:  planned.InputPath,
			OutputPath: planned.OutputPath,
		}

		outcome, optimizeErr := engine.OptimizeWithOptions(ctx, planned.InputPath, planned.OutputPath, optimizeEngineOptions(parsed))
		if optimizeErr != nil {
			jobErr := mapOptimizeError(optimizeErr)
			item.Success = false
			item.Message = jobErr.Message
			item.Error = jobErr
			fileErrors = append(fileErrors, map[string]any{
				"path":    planned.InputPath,
				"code":    jobErr.DetailCode,
				"message": jobErr.Message,
			})
		} else {
			item.Success = true
			item.Message = optimizeSuccessMessage(parsed, outcome)
			item.Outputs = []string{planned.OutputPath}
			item.OutputCount = 1
		}

		items = append(items, item)

		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: i + 1,
				Total:   len(plan.Outputs),
				Stage:   "running",
				Message: fmt.Sprintf("processed %d/%d", i+1, len(plan.Outputs)),
			})
		}
	}

	if len(fileErrors) > 0 {
		return items, models.NewCanonicalJobError(engine.ErrorCodeOptimizeFailed, "one or more files failed in batch optimize", map[string]any{
			"fileErrors": fileErrors,
		})
	}

	return items, nil
}

type optimizeRequestFields struct {
	Mode            string
	InputPath       string
	InputPaths      []string
	OutputPath      string
	OutputDir       string
	Compress        bool
	TargetSizeBytes int64
	Metadata        optimizeMetadata
}

type optimizeMetadata struct {
	Title    string
	Author   string
	Subject  string
	Keywords string
	Creator  string
}

// properties maps the tool-facing metadata keys to the PDF info dictionary
// keys pdfcpu expects. Empty values are omitted.
func (m optimizeMetadata) properties() map[string]string {
	properties := make(map[string]string)
	for key, value := range m.effective() {
		switch key {
		case "title":
			properties["Title"] = value
		case "author":
			properties["Author"] = value
		case "subject":
			properties["Subject"] = value
		case "keywords":
			properties["Keywords"] = value
		case "creator":
			properties["Creator"] = value
		}
	}

	return properties
}

// effective returns the supplied, non-empty metadata values keyed by their
// tool-facing names.
func (m optimizeMetadata) effective() map[string]string {
	values := make(map[string]string)
	for key, value := range map[string]string{
		"title":    m.Title,
		"author":   m.Author,
		"subject":  m.Subject,
		"keywords": m.Keywords,
		"creator":  m.Creator,
	} {
		if value != "" {
			values[key] = value
		}
	}

	return values
}

func optimizeReqFields(req models.JobRequestV1) (optimizeRequestFields, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return optimizeRequestFields{}, optimizeRequestError(engine.ErrorCodeValidation, "mode must be single or batch")
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return optimizeRequestFields{}, optimizeRequestError(engine.ErrorCodeValidation, "exactly 1 input PDF is required")
	}
	if mode == "batch" && len(req.InputPaths) < 1 {
		return optimizeRequestFields{}, optimizeRequestError(engine.ErrorCodeValidation, "at least 1 input PDF is required")
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInput := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return optimizeRequestFields{}, optimizeRequestError(engine.ErrorCodeValidation, "inputPath is required")
		}
		inputPaths = append(inputPaths, inputPath)
	}

	// The chosen folder is optional: without it every output is written next
	// to its input. req.OutputDir is the current field; options.outputDir stays
	// supported for older clients.
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = optionString(req.Options, "outputDir")
	}
	if outputDir != "" {
		outputDir = filepath.Clean(outputDir)
	}

	outputPath := strings.TrimSpace(optionString(req.Options, "outputPath"))
	if outputPath != "" && mode == "batch" {
		return optimizeRequestFields{}, optimizeRequestError(engine.ErrorCodeValidation, "options.outputPath is only supported for single mode")
	}

	compress, compressErr := optimizeOptionBool(req.Options, "compress")
	if compressErr != nil {
		return optimizeRequestFields{}, compressErr
	}

	targetSizeBytes, targetErr := optimizeOptionTargetSize(req.Options, "targetSizeBytes")
	if targetErr != nil {
		return optimizeRequestFields{}, targetErr
	}
	if targetSizeBytes > 0 && !compress {
		return optimizeRequestFields{}, optimizeTargetError("options.targetSizeBytes requires compression; enable compress or clear the target")
	}

	metadata, metadataErr := optimizeOptionMetadata(req.Options, "metadata")
	if metadataErr != nil {
		return optimizeRequestFields{}, metadataErr
	}

	return optimizeRequestFields{
		Mode:            mode,
		InputPath:       firstInputPath(inputPaths),
		InputPaths:      inputPaths,
		OutputPath:      outputPath,
		OutputDir:       outputDir,
		Compress:        compress,
		TargetSizeBytes: targetSizeBytes,
		Metadata:        metadata,
	}, nil
}

// optimizeOptionBool defaults compress to true when the option is absent.
func optimizeOptionBool(options map[string]any, key string) (bool, *models.JobErrorV1) {
	if options == nil {
		return true, nil
	}

	raw, ok := options[key]
	if !ok || raw == nil {
		return true, nil
	}

	switch value := raw.(type) {
	case bool:
		return value, nil
	case string:
		if strings.EqualFold(strings.TrimSpace(value), "true") {
			return true, nil
		}
		if strings.EqualFold(strings.TrimSpace(value), "false") {
			return false, nil
		}
	}

	return false, optimizeRequestError(engine.ErrorCodeValidation, "options.compress must be a boolean")
}

// optimizeOptionTargetSize parses options.targetSizeBytes. Absent or null means
// no target; numeric int/float values are accepted when positive and whole.
func optimizeOptionTargetSize(options map[string]any, key string) (int64, *models.JobErrorV1) {
	if options == nil {
		return 0, nil
	}

	raw, ok := options[key]
	if !ok || raw == nil {
		return 0, nil
	}

	switch value := raw.(type) {
	case int:
		if value <= 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return int64(value), nil
	case int8:
		if value <= 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return int64(value), nil
	case int16:
		if value <= 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return int64(value), nil
	case int32:
		if value <= 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return int64(value), nil
	case int64:
		if value <= 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return value, nil
	case uint:
		if value == 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return floatTargetSize(float64(value))
	case uint8:
		if value == 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return int64(value), nil
	case uint16:
		if value == 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return int64(value), nil
	case uint32:
		if value == 0 {
			return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
		}
		return int64(value), nil
	case uint64:
		return floatTargetSize(float64(value))
	case float32:
		return floatTargetSize(float64(value))
	case float64:
		return floatTargetSize(value)
	case json.Number:
		if integer, err := value.Int64(); err == nil {
			if integer <= 0 {
				return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
			}
			return integer, nil
		}
		parsed, err := value.Float64()
		if err != nil {
			return 0, optimizeTargetError("options.targetSizeBytes must be a positive number of bytes")
		}
		return floatTargetSize(parsed)
	default:
		return 0, optimizeTargetError("options.targetSizeBytes must be a positive number of bytes")
	}
}

func floatTargetSize(value float64) (int64, *models.JobErrorV1) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, optimizeTargetError("options.targetSizeBytes must be greater than 0")
	}
	if value > float64(math.MaxInt64) {
		return 0, optimizeTargetError("options.targetSizeBytes is too large")
	}
	if math.Trunc(value) != value {
		return 0, optimizeTargetError("options.targetSizeBytes must be a whole number of bytes")
	}

	return int64(value), nil
}

func optimizeTargetError(message string) *models.JobErrorV1 {
	return optimizeRequestError(engine.ErrorCodeOptimizeTargetInvalid, message)
}

func optimizeOptionMetadata(options map[string]any, key string) (optimizeMetadata, *models.JobErrorV1) {
	metadata := optimizeMetadata{}
	if options == nil {
		return metadata, nil
	}

	raw, ok := options[key]
	if !ok || raw == nil {
		return metadata, nil
	}

	values, ok := raw.(map[string]any)
	if !ok {
		return metadata, optimizeMetadataError("options.metadata must be an object")
	}

	fields := map[string]*string{
		"title":    &metadata.Title,
		"author":   &metadata.Author,
		"subject":  &metadata.Subject,
		"keywords": &metadata.Keywords,
		"creator":  &metadata.Creator,
	}

	for field, target := range fields {
		value, fieldErr := optimizeMetadataString(values, field)
		if fieldErr != nil {
			return optimizeMetadata{}, fieldErr
		}
		*target = value
	}

	return metadata, nil
}

func optimizeMetadataString(values map[string]any, field string) (string, *models.JobErrorV1) {
	raw, ok := values[field]
	if !ok || raw == nil {
		return "", nil
	}

	value, ok := raw.(string)
	if !ok {
		return "", optimizeMetadataError(fmt.Sprintf("options.metadata.%s must be a string", field))
	}

	value = strings.TrimSpace(value)
	if len([]rune(value)) > engine.OptimizeMetadataMaxLength {
		return "", optimizeMetadataError(fmt.Sprintf("options.metadata.%s must be at most %d characters", field, engine.OptimizeMetadataMaxLength))
	}

	return value, nil
}

func optimizeMetadataError(message string) *models.JobErrorV1 {
	return optimizeRequestError(engine.ErrorCodeOptimizeMetadataInvalid, message)
}

// planExplicitOptimizeOutput resolves the one output of a single request that
// supplied options.outputPath.
func planExplicitOptimizeOutput(parsed optimizeRequestFields) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:   parsed.InputPath,
		OutputPath:  parsed.OutputPath,
		OutputKind:  optimizeOutputKindFile,
		OutputCount: 1,
	}

	if optimizeErr := engine.ValidateOptimizeRequest(parsed.InputPath, parsed.OutputPath); optimizeErr != nil {
		return planned, mapOptimizeError(optimizeErr)
	}

	return planned, nil
}

func planAutoOptimizeOutput(inputPath, dir string, used map[string]struct{}) models.PlannedOutputV1 {
	planned := models.PlannedOutputV1{
		InputPath:   inputPath,
		OutputKind:  optimizeOutputKindFile,
		OutputCount: 1,
	}

	base := optimizeOutputBaseName(inputPath)
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, "pdf", used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+".pdf", filepath.Base(outputPath)))
	}

	return planned
}

func optimizeOutputBaseName(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "document"
	}

	return stem + optimizeOutputSuffix
}

func optimizeEffectiveOptions(parsed optimizeRequestFields) map[string]any {
	effective := map[string]any{"compress": parsed.Compress}
	if parsed.TargetSizeBytes > 0 {
		effective["targetSizeBytes"] = parsed.TargetSizeBytes
	}
	if metadata := parsed.Metadata.effective(); len(metadata) > 0 {
		effective["metadata"] = metadata
	}

	return effective
}

func optimizeEngineOptions(parsed optimizeRequestFields) engine.OptimizeOptions {
	return engine.OptimizeOptions{
		Compress:        parsed.Compress,
		Properties:      parsed.Metadata.properties(),
		TargetSizeBytes: parsed.TargetSizeBytes,
	}
}

// optimizeTargetWarnings explains the best-effort target policy before a run.
func optimizeTargetWarnings(parsed optimizeRequestFields) []string {
	if parsed.TargetSizeBytes <= 0 {
		return nil
	}

	warnings := []string{
		fmt.Sprintf("Compression to %s is best effort; the target may not be reachable without rasterizing pages.", formatByteSize(parsed.TargetSizeBytes)),
	}
	if !engine.GhostscriptLookPath() {
		warnings = append(warnings, "Ghostscript is not available; only pdfcpu compression will be tried.")
	}

	return warnings
}

func optimizePlanMessage(count int) string {
	if count == 1 {
		return "Ready to optimize 1 PDF."
	}

	return fmt.Sprintf("Ready to optimize %d PDFs.", count)
}

func optimizeSuccessMessage(parsed optimizeRequestFields, outcome engine.OptimizeOutcome) string {
	if parsed.TargetSizeBytes <= 0 {
		if parsed.Compress && len(parsed.Metadata.properties()) > 0 {
			return "PDF optimized and metadata updated"
		}
		if parsed.Compress {
			return "PDF optimize successful"
		}
		if len(parsed.Metadata.properties()) > 0 {
			return "PDF metadata updated"
		}

		return "PDF copied without changes"
	}

	actual := formatByteSize(outcome.SizeBytes)
	target := formatByteSize(parsed.TargetSizeBytes)
	if outcome.TargetMet {
		return fmt.Sprintf("PDF optimized to %s; target of %s met", actual, target)
	}

	message := fmt.Sprintf("PDF optimized to %s; the target of %s could not be met (best effort)", actual, target)
	if !outcome.GhostscriptAvailable {
		message += ". Ghostscript is not available; install it for stronger compression"
	} else if outcome.GhostscriptError != "" {
		message += ". Ghostscript compression failed"
	}

	return message
}

// formatByteSize renders a byte count with a unit the UI can show as-is.
func formatByteSize(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}

	const (
		kib = 1024.0
		mib = kib * 1024.0
		gib = mib * 1024.0
	)

	switch {
	case float64(bytes) >= gib:
		return fmt.Sprintf("%.1f GB", float64(bytes)/gib)
	case float64(bytes) >= mib:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mib)
	case float64(bytes) >= kib:
		return fmt.Sprintf("%.0f KB", float64(bytes)/kib)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func optimizeFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
}

func optimizePlannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func optimizeRequestError(code, message string) *models.JobErrorV1 {
	return &models.JobErrorV1{Code: code, DetailCode: code, Message: message}
}

func mapOptimizeError(err error) *models.JobErrorV1 {
	var optimizeErr *engine.OptimizeError
	if !errors.As(err, &optimizeErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return &models.JobErrorV1{Code: optimizeErr.Code, DetailCode: optimizeErr.Code, Message: optimizeErr.Message}
}
