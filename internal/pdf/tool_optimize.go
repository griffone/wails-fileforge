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
	"fileforge-desktop/internal/utils"
)

const (
	ToolIDPDFOptimize = "tool.pdf.optimize"

	optimizeOutputKindFile = "file"
	optimizeOutputSuffix   = "_optimized"
)

type OptimizeTool struct{}

func NewOptimizeTool() *OptimizeTool {
	return &OptimizeTool{}
}

func (t *OptimizeTool) ID() string {
	return ToolIDPDFOptimize
}

func (t *OptimizeTool) Capability() string {
	return ToolIDPDFOptimize
}

func (t *OptimizeTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "PDF Optimize",
		Description:      "Compress PDFs and edit document metadata",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "optimize", "compress", "metadata"},
	}
}

func (t *OptimizeTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *OptimizeTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_optimized.pdf destination per input
// without writing anything. An explicit options.outputPath is only accepted for
// single mode; it must be a non-existing .pdf in a writable directory.
func (t *OptimizeTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

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
				plan.Outputs = []models.PlannedOutput{planned}
			}
			return planFailure(plan, planErr)
		}

		plan.Success = true
		plan.Message = optimizePlanMessage(1)
		plan.Outputs = []models.PlannedOutput{planned}
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

	outputs := make([]models.PlannedOutput, 0, len(parsed.InputPaths))
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

// ExecuteSingle keeps the legacy single-executor contract by resolving the plan
// first and then running that exact plan, so it cannot drift from ExecutePlan.
func (t *OptimizeTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
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

	items, execErr := t.ExecutePlan(ctx, req, plan, nil)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("PDF_OPTIMIZE_EXECUTION_MISSING_RESULT", "optimize execution returned no result", nil)
	}

	return optimizeFailureItem(parsed.InputPath, optimizePlannedOutputPath(plan), jobErr), jobErr
}

// ExecuteBatch plans the request once and then executes exactly that plan. It
// never re-resolves output paths from the request.
func (t *OptimizeTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
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

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan optimizes exactly the outputs resolved by Plan. Options come from
// the plan's effective values, so a reduced retry plan only processes its own
// outputs. Results keep plan order.
func (t *OptimizeTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := optimizeReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	parsed = plannedOptimizeOptions(plan, parsed)

	items := make([]models.JobResultItem, len(plan.Outputs))
	itemErrors := make([]*models.JobError, len(plan.Outputs))
	completed := make([]bool, len(plan.Outputs))
	progress := newPDFBatchProgress(len(plan.Outputs), onProgress)

	runErr := utils.ParallelForIndex(ctx, len(plan.Outputs), pdfBatchWorkerCount(), func(ctx context.Context, i int) error {
		planned := plan.Outputs[i]
		item := models.JobResultItem{
			InputPath:  planned.InputPath,
			OutputPath: planned.OutputPath,
		}

		outcome, optimizeErr := engine.OptimizeWithOptions(ctx, planned.InputPath, planned.OutputPath, optimizeEngineOptions(parsed))
		if optimizeErr != nil {
			jobErr := mapOptimizeError(optimizeErr)
			item.Success = false
			item.Message = jobErr.Message
			item.Error = jobErr
			itemErrors[i] = jobErr
		} else {
			item.Success = true
			item.Message = optimizeSuccessMessage(parsed, outcome)
			item.Outputs = []string{planned.OutputPath}
			item.OutputCount = 1
		}

		items[i] = item
		completed[i] = true
		progress.itemCompleted()

		return nil
	})

	if runErr != nil {
		if ctx.Err() != nil {
			return completedPDFBatchItems(items, completed), &models.JobError{Code: "CANCELED", Message: ctx.Err().Error()}
		}

		return completedPDFBatchItems(items, completed), models.NewCanonicalJobError(engine.ErrorCodeOptimizeFailed, "batch optimize failed", map[string]any{"cause": runErr.Error()})
	}

	fileErrors := make([]map[string]any, 0)
	for i, jobErr := range itemErrors {
		if jobErr == nil {
			continue
		}
		fileErrors = append(fileErrors, map[string]any{
			"path":    plan.Outputs[i].InputPath,
			"code":    jobErr.DetailCode,
			"message": jobErr.Message,
		})
	}

	if len(fileErrors) > 0 {
		return items, models.NewCanonicalJobError(engine.ErrorCodeOptimizeFailed, "one or more files failed in batch optimize", map[string]any{
			"fileErrors": fileErrors,
		})
	}

	return items, nil
}

// plannedOptimizeOptions returns the optimize options resolved by Plan, falling
// back to the re-parsed request when a plan was built by an older caller.
func plannedOptimizeOptions(plan models.PlanJobResponse, parsed optimizeRequestFields) optimizeRequestFields {
	if plan.EffectiveOptions == nil {
		return parsed
	}

	if compress, ok := planBoolOption(plan.EffectiveOptions, "compress"); ok {
		parsed.Compress = compress
	}
	if targetSize, ok := planInt64Option(plan.EffectiveOptions, "targetSizeBytes"); ok && targetSize > 0 {
		parsed.TargetSizeBytes = targetSize
	}
	if metadata, ok := plannedOptimizeMetadata(plan.EffectiveOptions["metadata"]); ok {
		parsed.Metadata = metadata
	}

	return parsed
}

// plannedOptimizeMetadata reads the metadata map stored in the plan. It accepts
// both the in-memory map[string]string shape and a JSON-roundtripped map.
func plannedOptimizeMetadata(raw any) (optimizeMetadata, bool) {
	switch values := raw.(type) {
	case map[string]string:
		return optimizeMetadata{
			Title:    values["title"],
			Author:   values["author"],
			Subject:  values["subject"],
			Keywords: values["keywords"],
			Creator:  values["creator"],
		}, true
	case map[string]any:
		read := func(key string) string {
			value, _ := values[key].(string)
			return value
		}
		return optimizeMetadata{
			Title:    read("title"),
			Author:   read("author"),
			Subject:  read("subject"),
			Keywords: read("keywords"),
			Creator:  read("creator"),
		}, true
	default:
		return optimizeMetadata{}, false
	}
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

func optimizeReqFields(req models.JobRequest) (optimizeRequestFields, *models.JobError) {
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
func optimizeOptionBool(options map[string]any, key string) (bool, *models.JobError) {
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
func optimizeOptionTargetSize(options map[string]any, key string) (int64, *models.JobError) {
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

func floatTargetSize(value float64) (int64, *models.JobError) {
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

func optimizeTargetError(message string) *models.JobError {
	return optimizeRequestError(engine.ErrorCodeOptimizeTargetInvalid, message)
}

func optimizeOptionMetadata(options map[string]any, key string) (optimizeMetadata, *models.JobError) {
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

func optimizeMetadataString(values map[string]any, field string) (string, *models.JobError) {
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

func optimizeMetadataError(message string) *models.JobError {
	return optimizeRequestError(engine.ErrorCodeOptimizeMetadataInvalid, message)
}

// planExplicitOptimizeOutput resolves the one output of a single request that
// supplied options.outputPath.
func planExplicitOptimizeOutput(parsed optimizeRequestFields) (models.PlannedOutput, *models.JobError) {
	planned := models.PlannedOutput{
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

func planAutoOptimizeOutput(inputPath, dir string, used map[string]struct{}) models.PlannedOutput {
	planned := models.PlannedOutput{
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

func optimizeFailureItem(inputPath, outputPath string, jobErr *models.JobError) models.JobResultItem {
	return models.JobResultItem{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
}

func optimizePlannedOutputPath(plan models.PlanJobResponse) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func optimizeRequestError(code, message string) *models.JobError {
	return &models.JobError{Code: code, DetailCode: code, Message: message}
}

func mapOptimizeError(err error) *models.JobError {
	var optimizeErr *engine.OptimizeError
	if !errors.As(err, &optimizeErr) {
		return &models.JobError{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return &models.JobError{Code: optimizeErr.Code, DetailCode: optimizeErr.Code, Message: optimizeErr.Message}
}
