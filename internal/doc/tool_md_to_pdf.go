package doc

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/utils"
)

const (
	ToolIDDocMDToPDFV1 = "tool.doc.md_to_pdf"

	docPlannedOutputKindFile = "file"

	mdMaxPageMarginMM = 100.0

	// mdRenderWorkers bounds concurrent Markdown renders. Rendering is
	// CPU-bound, so two workers keep the machine busy without starving the UI.
	mdRenderWorkers = 2
)

var placeholderPattern = regexp.MustCompile(`\{([^}]+)\}`)

var allowedPlaceholders = map[string]struct{}{
	"page":       {},
	"totalPages": {},
	"date":       {},
	"fileName":   {},
}

type MDToPDFTool struct{}

func NewMDToPDFTool() *MDToPDFTool {
	return &MDToPDFTool{}
}

func (t *MDToPDFTool) ID() string {
	return ToolIDDocMDToPDFV1
}

func (t *MDToPDFTool) Capability() string {
	return ToolIDDocMDToPDFV1
}

func (t *MDToPDFTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Markdown to PDF",
		Description:      "Convert Markdown documents to PDF with optional header/footer",
		Domain:           "doc",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"md", "markdown"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{},
		ResourceClass:    "cpu",
		MaxWorkers:       2,
		Tags:             []string{"doc", "markdown", "pdf", "header", "footer"},
	}
}

func (t *MDToPDFTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *MDToPDFTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe Markdown to PDF destination per input
// without writing anything. An explicit options.outputPath is only accepted for
// single mode. Execution reuses the planned request options, so the plan and
// the run render with the exact same page setup.
func (t *MDToPDFTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	prepared, jobErr := parseAndPrepareBatch(req)
	if jobErr != nil {
		return docPlanFailure(plan, jobErr)
	}

	explicitOutput := ""
	if strings.TrimSpace(req.Mode) == "single" {
		explicitOutput = optionString(req.Options, "outputPath")
	}

	outputs := make([]models.PlannedOutputV1, 0, len(prepared))
	warnings := make([]string, 0)
	for _, item := range prepared {
		naturalPath := naturalMDOutputPath(item.inputPath, req.OutputDir, explicitOutput)
		planned := models.PlannedOutputV1{
			InputPath:  item.inputPath,
			OutputPath: item.outputPath,
			OutputKind: docPlannedOutputKindFile,
		}
		if !fsx.SameFile(naturalPath, item.outputPath) {
			planned.Adjusted = true
			planned.Warnings = append(planned.Warnings, fmt.Sprintf(
				"%s already exists. Saving as %s.",
				filepath.Base(naturalPath),
				filepath.Base(item.outputPath),
			))
		}

		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = mdPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = mdEffectiveOptions(req.Options, prepared[0].renderConfig)

	return plan, nil
}

// ExecuteSingle resolves the plan first and then runs that exact plan, so it
// cannot drift from ExecutePlan.
func (t *MDToPDFTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return mdFailureItem(firstPath(req.InputPaths), mdPlannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, nil)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("DOC_MD_TO_PDF_RENDER_FAILED", "markdown render returned no result", nil)
	}

	return mdFailureItem(firstPath(req.InputPaths), mdPlannedOutputPath(plan), jobErr), jobErr
}

// ExecuteBatch plans the whole request once and then executes exactly that
// plan. It never re-resolves output paths from the request.
func (t *MDToPDFTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan renders exactly the outputs resolved by Plan. It never re-plans
// or re-derives output paths from req.Options, so a reduced retry plan only
// processes its own outputs. Results keep plan order.
func (t *MDToPDFTool) ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	total := len(plan.Outputs)
	if total == 0 {
		return nil, models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_REQUIRED", "at least one markdown input is required", nil)
	}

	renderOptions, optionsErr := parseMDRenderOptions(req.Options)
	if optionsErr != nil {
		return nil, optionsErr
	}

	items := make([]models.JobResultItemV1, total)
	completed := make([]bool, total)
	progress := newDocItemProgress(total, onProgress)

	runErr := utils.ParallelForIndex(ctx, total, mdRenderWorkers, func(ctx context.Context, index int) error {
		planned := plan.Outputs[index]
		config := renderOptions.renderConfig(planned.InputPath, planned.OutputPath)

		if err := engine.RenderMarkdownToPDF(ctx, config); err != nil {
			jobErr := models.NewCanonicalJobError("DOC_MD_TO_PDF_RENDER_FAILED", err.Error(), map[string]any{"inputPath": planned.InputPath})
			items[index] = mdFailureItem(planned.InputPath, planned.OutputPath, jobErr)
		} else {
			items[index] = models.JobResultItemV1{
				InputPath:   planned.InputPath,
				OutputPath:  planned.OutputPath,
				Outputs:     []string{planned.OutputPath},
				OutputCount: 1,
				Success:     true,
				Message:     "markdown to pdf successful",
			}
		}

		completed[index] = true
		progress.itemCompleted()

		return nil
	})

	if docBatchCancelled(ctx, runErr) {
		return collectCompletedDocItems(items, completed), models.NewCanonicalJobError("DOC_MD_TO_PDF_CANCELLED", docBatchCancellationCause(ctx, runErr).Error(), nil)
	}
	if runErr != nil {
		return collectCompletedDocItems(items, completed), models.NewCanonicalJobError("DOC_MD_TO_PDF_RENDER_FAILED", runErr.Error(), nil)
	}

	return items, firstFailedDocItemError(items)
}

type preparedRequest struct {
	inputPath    string
	outputPath   string
	renderConfig engine.RenderConfig
}

// mdRenderOptions holds the request options shared by every render. They are
// parsed once per request, never per input.
type mdRenderOptions struct {
	pageSetup mdPageSetup
	header    engine.HeaderFooterConfig
	footer    engine.HeaderFooterConfig
}

// renderConfig builds the engine configuration for one planned output. The
// input and output paths always come from the plan.
func (o mdRenderOptions) renderConfig(inputPath, outputPath string) engine.RenderConfig {
	return engine.RenderConfig{
		InputPath:      inputPath,
		OutputPath:     outputPath,
		PageSize:       o.pageSetup.PageSize,
		Orientation:    o.pageSetup.Orientation,
		MarginTopMM:    o.pageSetup.MarginTopMM,
		MarginRightMM:  o.pageSetup.MarginRightMM,
		MarginBottomMM: o.pageSetup.MarginBottomMM,
		MarginLeftMM:   o.pageSetup.MarginLeftMM,
		Header:         o.header,
		Footer:         o.footer,
	}
}

func parseMDRenderOptions(options map[string]any) (mdRenderOptions, *models.JobErrorV1) {
	header, err := parseHeaderFooterOption(options, "header")
	if err != nil {
		return mdRenderOptions{}, err
	}

	footer, err := parseHeaderFooterOption(options, "footer")
	if err != nil {
		return mdRenderOptions{}, err
	}

	pageSetup, setupErr := parsePageSetupOption(options)
	if setupErr != nil {
		return mdRenderOptions{}, setupErr
	}

	return mdRenderOptions{pageSetup: pageSetup, header: header, footer: footer}, nil
}

// parseAndPrepareBatch validates the request and resolves one collision-safe
// output per input. In single mode an explicit options.outputPath is honored;
// batch mode always derives per-input names.
func parseAndPrepareBatch(req models.JobRequestV1) ([]preparedRequest, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return nil, models.NewCanonicalJobError("DOC_MD_TO_PDF_MODE_INVALID", "mode must be single or batch", nil)
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return nil, models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_COUNT_INVALID", "single mode requires exactly one markdown input", nil)
	}
	if mode == "batch" && len(req.InputPaths) < 1 {
		return nil, models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_COUNT_INVALID", "batch mode requires at least one markdown input", nil)
	}

	explicitOutput := optionString(req.Options, "outputPath")
	if mode == "batch" && explicitOutput != "" {
		return nil, models.NewCanonicalJobError("DOC_MD_TO_PDF_OUTPUT_INVALID", "options.outputPath is only supported in single mode", nil)
	}

	renderOptions, optionsErr := parseMDRenderOptions(req.Options)
	if optionsErr != nil {
		return nil, optionsErr
	}

	inputs := make([]string, 0, len(req.InputPaths))
	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(req.InputPaths))
	for _, raw := range req.InputPaths {
		inputPath := strings.TrimSpace(raw)
		if inputPath == "" {
			return nil, models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_REQUIRED", "inputPath is required", nil)
		}
		if err := validateInputPath(inputPath); err != nil {
			return nil, err
		}
		inputs = append(inputs, inputPath)
		outputplan.MarkUsed(used, inputPath)
	}

	prepared := make([]preparedRequest, 0, len(inputs))
	for _, inputPath := range inputs {
		outputPath, outputErr := resolveBatchOutputPath(inputPath, req.OutputDir, explicitOutput, used)
		if outputErr != nil {
			return nil, outputErr
		}

		if fsx.SameFile(inputPath, outputPath) {
			return nil, models.NewCanonicalJobError("DOC_MD_TO_PDF_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
		}

		prepared = append(prepared, preparedRequest{
			inputPath:    inputPath,
			outputPath:   outputPath,
			renderConfig: renderOptions.renderConfig(inputPath, outputPath),
		})
	}

	return prepared, nil
}

// mdPageSetup is the resolved page configuration for one Markdown render.
type mdPageSetup struct {
	PageSize       string
	Orientation    string
	MarginTopMM    float64
	MarginRightMM  float64
	MarginBottomMM float64
	MarginLeftMM   float64
}

// parsePageSetupOption resolves pageSize, orientation and margins. Missing
// values keep the documented defaults (A4 portrait, 20/15/18/15 mm).
func parsePageSetupOption(options map[string]any) (mdPageSetup, *models.JobErrorV1) {
	setup := mdPageSetup{
		PageSize:       engine.DefaultPageSize,
		Orientation:    engine.DefaultOrientation,
		MarginTopMM:    engine.DefaultMarginTopMM,
		MarginRightMM:  engine.DefaultMarginRightMM,
		MarginBottomMM: engine.DefaultMarginBottomMM,
		MarginLeftMM:   engine.DefaultMarginLeftMM,
	}

	if options == nil {
		return setup, nil
	}

	if raw, ok := options["pageSize"]; ok && raw != nil {
		value, isString := raw.(string)
		if !isString {
			return mdPageSetup{}, mdConfigError("options.pageSize must be a string")
		}
		if strings.TrimSpace(value) != "" {
			normalized, valid := engine.NormalizePageSize(value)
			if !valid {
				return mdPageSetup{}, mdConfigError("options.pageSize must be A4, Letter or Legal")
			}
			setup.PageSize = normalized
		}
	}

	if raw, ok := options["orientation"]; ok && raw != nil {
		value, isString := raw.(string)
		if !isString {
			return mdPageSetup{}, mdConfigError("options.orientation must be a string")
		}
		if strings.TrimSpace(value) != "" {
			normalized, valid := engine.NormalizeOrientation(value)
			if !valid {
				return mdPageSetup{}, mdConfigError("options.orientation must be P (portrait) or L (landscape)")
			}
			setup.Orientation = normalized
		}
	}

	if raw, ok := options["margins"]; ok && raw != nil {
		mapped, isObject := raw.(map[string]any)
		if !isObject {
			return mdPageSetup{}, mdConfigError("options.margins must be an object with numeric top, right, bottom and left values")
		}

		targets := []struct {
			name string
			dest *float64
		}{
			{name: "top", dest: &setup.MarginTopMM},
			{name: "right", dest: &setup.MarginRightMM},
			{name: "bottom", dest: &setup.MarginBottomMM},
			{name: "left", dest: &setup.MarginLeftMM},
		}

		for _, target := range targets {
			value, exists := mapped[target.name]
			if !exists || value == nil {
				continue
			}
			if !isNumeric(value) {
				return mdPageSetup{}, mdConfigError(fmt.Sprintf("options.margins.%s must be numeric", target.name))
			}

			margin := mustFloat(mapped, target.name)
			if math.IsNaN(margin) || math.IsInf(margin, 0) || margin < 0 || margin > mdMaxPageMarginMM {
				return mdPageSetup{}, mdConfigError(fmt.Sprintf("options.margins.%s must be between 0 and %.0f mm", target.name, mdMaxPageMarginMM))
			}

			*target.dest = margin
		}
	}

	return setup, nil
}

func mdConfigError(message string) *models.JobErrorV1 {
	return models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", message, nil)
}

func validateInputPath(inputPath string) *models.JobErrorV1 {
	name := filepath.Base(inputPath)

	info, err := os.Stat(inputPath)
	if err != nil {
		return models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_NOT_FOUND", fmt.Sprintf("%s was not found.", name), map[string]any{"inputPath": inputPath})
	}

	if info.IsDir() {
		return models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_IS_DIR", fmt.Sprintf("%s is a folder, not a Markdown file.", name), map[string]any{"inputPath": inputPath})
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(inputPath)), ".")
	if ext != "md" && ext != "markdown" {
		return models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_UNSUPPORTED", fmt.Sprintf("%s must be a .md or .markdown file.", name), map[string]any{"inputPath": inputPath})
	}

	return nil
}

// naturalMDOutputPath returns the output path the user would expect before any
// collision suffix is applied: the explicit outputPath when given, otherwise
// <stem>_md2pdf.pdf inside the target directory.
func naturalMDOutputPath(inputPath, outputDir, rawOutputPath string) string {
	if explicit := strings.TrimSpace(rawOutputPath); explicit != "" {
		return filepath.Clean(explicit)
	}

	targetDir := strings.TrimSpace(outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}

	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(base) == "" {
		base = "document"
	}

	return filepath.Join(targetDir, base+"_md2pdf.pdf")
}

// mdEffectiveOptions summarizes the resolved page setup plus any header/footer
// configuration. The page setup is always present because defaults apply.
func mdEffectiveOptions(options map[string]any, cfg engine.RenderConfig) map[string]any {
	effective := map[string]any{
		"pageSetup": map[string]any{
			"pageSize":    cfg.PageSize,
			"orientation": cfg.Orientation,
			"margins": map[string]any{
				"top":    cfg.MarginTopMM,
				"right":  cfg.MarginRightMM,
				"bottom": cfg.MarginBottomMM,
				"left":   cfg.MarginLeftMM,
			},
		},
	}

	if raw, ok := options["header"]; ok && raw != nil {
		effective["header"] = headerFooterSummary(cfg.Header)
	}
	if raw, ok := options["footer"]; ok && raw != nil {
		effective["footer"] = headerFooterSummary(cfg.Footer)
	}

	return effective
}

func headerFooterSummary(cfg engine.HeaderFooterConfig) map[string]any {
	return map[string]any{
		"enabled":      cfg.Enabled,
		"text":         cfg.Text,
		"align":        cfg.Align,
		"font":         cfg.Font,
		"marginTop":    cfg.MarginTop,
		"marginBottom": cfg.MarginBottom,
		"color":        cfg.Color,
	}
}

// docPlanFailure attaches a canonical error to a plan that could not resolve.
func docPlanFailure(plan models.PlanJobResponseV1, jobErr *models.JobErrorV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}

	return plan, jobErr
}

func parseHeaderFooterOption(options map[string]any, key string) (engine.HeaderFooterConfig, *models.JobErrorV1) {
	if options == nil {
		return engine.HeaderFooterConfig{}, nil
	}

	raw, ok := options[key]
	if !ok || raw == nil {
		return engine.HeaderFooterConfig{}, nil
	}

	mapped, ok := raw.(map[string]any)
	if !ok {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s must be an object", key), nil)
	}

	requiredKeys := []string{"enabled", "text", "align", "font", "marginTop", "marginBottom", "color"}
	for _, reqKey := range requiredKeys {
		if _, exists := mapped[reqKey]; !exists {
			return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s.%s is required", key, reqKey), nil)
		}
	}

	if _, ok := mapped["text"].(string); !ok {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s.text must be string", key), nil)
	}
	if _, ok := mapped["align"].(string); !ok {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s.align must be string", key), nil)
	}
	if _, ok := mapped["font"].(string); !ok {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s.font must be string", key), nil)
	}
	if _, ok := mapped["color"].(string); !ok {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s.color must be string", key), nil)
	}

	enabled, ok := mapped["enabled"].(bool)
	if !ok {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s.enabled must be boolean", key), nil)
	}

	if !isNumeric(mapped["marginTop"]) {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s.marginTop must be numeric", key), nil)
	}

	if !isNumeric(mapped["marginBottom"]) {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_CONFIG_INVALID", fmt.Sprintf("options.%s.marginBottom must be numeric", key), nil)
	}

	cfg := engine.HeaderFooterConfig{
		Enabled:      enabled,
		Text:         optionString(mapped, "text"),
		Align:        strings.ToLower(optionString(mapped, "align")),
		Font:         strings.ToLower(optionString(mapped, "font")),
		MarginTop:    mustFloat(mapped, "marginTop"),
		MarginBottom: mustFloat(mapped, "marginBottom"),
		Color:        optionString(mapped, "color"),
	}

	if cfg.Align == "" {
		cfg.Align = "left"
	}
	if cfg.Font == "" {
		cfg.Font = "helvetica"
	}
	if cfg.Color == "" {
		cfg.Color = "#000000"
	}

	if cfg.Align != "left" && cfg.Align != "center" && cfg.Align != "right" {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_ALIGN_INVALID", fmt.Sprintf("options.%s.align must be left, center or right", key), nil)
	}

	if cfg.Font != "helvetica" && cfg.Font != "times" && cfg.Font != "courier" {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_FONT_INVALID", fmt.Sprintf("options.%s.font must be helvetica, times or courier", key), nil)
	}

	if !isHexColor(cfg.Color) {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_COLOR_INVALID", fmt.Sprintf("options.%s.color must use #RRGGBB", key), nil)
	}

	if cfg.MarginTop < 0 {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_MARGIN_INVALID", fmt.Sprintf("options.%s.marginTop must be >= 0", key), nil)
	}

	if cfg.MarginBottom < 0 {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_MARGIN_INVALID", fmt.Sprintf("options.%s.marginBottom must be >= 0", key), nil)
	}

	if placeholderErr := validatePlaceholders(cfg.Text); placeholderErr != nil {
		return engine.HeaderFooterConfig{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_PLACEHOLDER_INVALID", fmt.Sprintf("options.%s.text %s", key, placeholderErr.Error()), nil)
	}

	return cfg, nil
}

func validatePlaceholders(text string) error {
	matches := placeholderPattern.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if _, ok := allowedPlaceholders[match[1]]; !ok {
			return fmt.Errorf("contains unsupported placeholder {%s}", match[1])
		}
	}
	return nil
}

// resolveBatchOutputPath resolves the destination for one input. It reserves
// the chosen name in used so parallel inputs never share an output path.
func resolveBatchOutputPath(inputPath, outputDir, explicitOutput string, used map[string]struct{}) (string, *models.JobErrorV1) {
	if strings.TrimSpace(explicitOutput) != "" {
		requestedPath := filepath.Clean(strings.TrimSpace(explicitOutput))
		if strings.ToLower(filepath.Ext(requestedPath)) != ".pdf" {
			return "", models.NewCanonicalJobError("DOC_MD_TO_PDF_OUTPUT_INVALID", "outputPath must use .pdf extension", nil)
		}

		targetDir := filepath.Dir(requestedPath)
		if dirErr := validateOutputDir(targetDir); dirErr != nil {
			return "", dirErr
		}

		stem := strings.TrimSuffix(filepath.Base(requestedPath), filepath.Ext(requestedPath))
		if strings.TrimSpace(stem) == "" {
			stem = "document"
		}

		outputPath, _ := outputplan.UniqueFilePath(targetDir, stem, "pdf", used)
		return outputPath, nil
	}

	targetDir := strings.TrimSpace(outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}

	if dirErr := validateOutputDir(targetDir); dirErr != nil {
		return "", dirErr
	}

	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(base) == "" {
		base = "document"
	}

	outputPath, _ := outputplan.UniqueFilePath(targetDir, base+"_md2pdf", "pdf", used)
	return outputPath, nil
}

func mdPlanMessage(count int) string {
	if count == 1 {
		return "Ready to convert 1 Markdown document."
	}

	return fmt.Sprintf("Ready to convert %d Markdown documents.", count)
}

func mdFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
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

func mdPlannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func validateOutputDir(dir string) *models.JobErrorV1 {
	if strings.TrimSpace(dir) == "" {
		return models.NewCanonicalJobError("DOC_MD_TO_PDF_OUTPUT_DIR_INVALID", "output directory is required", nil)
	}

	stat, err := os.Stat(dir)
	if err != nil {
		return models.NewCanonicalJobError("DOC_MD_TO_PDF_OUTPUT_DIR_INVALID", err.Error(), nil)
	}
	if !stat.IsDir() {
		return models.NewCanonicalJobError("DOC_MD_TO_PDF_OUTPUT_DIR_INVALID", "output directory must be a directory", nil)
	}

	return nil
}

func optionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	v, _ := options[key].(string)
	return strings.TrimSpace(v)
}

func optionBool(options map[string]any, key string) bool {
	if options == nil {
		return false
	}
	v, ok := options[key].(bool)
	if ok {
		return v
	}
	return false
}

func mustFloat(options map[string]any, key string) float64 {
	v := options[key]
	switch casted := v.(type) {
	case float64:
		return casted
	case float32:
		return float64(casted)
	case int:
		return float64(casted)
	case int64:
		return float64(casted)
	case int32:
		return float64(casted)
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(casted), 64)
		if err == nil {
			return n
		}
	}

	return 0
}

func isNumeric(v any) bool {
	switch v.(type) {
	case float64, float32, int, int32, int64:
		return true
	case string:
		_, err := strconv.ParseFloat(strings.TrimSpace(v.(string)), 64)
		return err == nil
	default:
		return false
	}
}

func optionFloat(options map[string]any, key string) float64 {
	if options == nil {
		return 0
	}

	v := options[key]
	switch casted := v.(type) {
	case float64:
		return casted
	case float32:
		return float64(casted)
	case int:
		return float64(casted)
	case int64:
		return float64(casted)
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(casted), 64)
		if err == nil {
			return n
		}
	}

	return 0
}

func isHexColor(value string) bool {
	v := strings.TrimSpace(value)
	if len(v) != 7 || !strings.HasPrefix(v, "#") {
		return false
	}

	for _, ch := range v[1:] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
			return false
		}
	}

	return true
}

func firstPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return strings.TrimSpace(paths[0])
}
