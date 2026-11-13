package doc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"
)

const (
	ToolIDDocMDToPDFV1 = "tool.doc.md_to_pdf"

	docPlannedOutputKindFile = "file"
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
		SupportsBatch:    false,
		InputExtensions:  []string{"md", "markdown"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{},
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

// Plan resolves the Markdown to PDF destination without writing anything.
// Execution reuses parseAndPrepare, so the plan and the run resolve the exact
// same collision-safe output path.
func (t *MDToPDFTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	prepared, jobErr := parseAndPrepare(req)
	if jobErr != nil {
		return docPlanFailure(plan, jobErr)
	}

	naturalPath := naturalMDOutputPath(prepared.inputPath, req.OutputDir, optionString(req.Options, "outputPath"))
	adjusted := !sameFile(naturalPath, prepared.outputPath)

	planned := models.PlannedOutputV1{
		InputPath:  prepared.inputPath,
		OutputPath: prepared.outputPath,
		OutputKind: docPlannedOutputKindFile,
	}
	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf(
			"%s already exists. Saving as %s.",
			filepath.Base(naturalPath),
			filepath.Base(prepared.outputPath),
		))
	}

	plan.Success = true
	plan.Message = "Ready to convert 1 Markdown document."
	plan.Outputs = []models.PlannedOutputV1{planned}
	plan.Warnings = planned.Warnings
	plan.EffectiveOptions = mdEffectiveOptions(req.Options, prepared.renderConfig)

	return plan, nil
}

func (t *MDToPDFTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	prepared, jobErr := parseAndPrepare(req)
	if jobErr != nil {
		return models.JobResultItemV1{InputPath: firstPath(req.InputPaths), OutputPath: "", Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	if err := engine.RenderMarkdownToPDF(ctx, prepared.renderConfig); err != nil {
		renderErr := models.NewCanonicalJobError("DOC_MD_TO_PDF_RENDER_FAILED", err.Error(), map[string]any{"inputPath": prepared.inputPath})
		return models.JobResultItemV1{
			InputPath:  prepared.inputPath,
			OutputPath: prepared.outputPath,
			Success:    false,
			Message:    renderErr.Message,
			Error:      renderErr,
		}, renderErr
	}

	return models.JobResultItemV1{
		InputPath:   prepared.inputPath,
		OutputPath:  prepared.outputPath,
		Outputs:     []string{prepared.outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "markdown to pdf successful",
	}, nil
}

type preparedRequest struct {
	inputPath    string
	outputPath   string
	renderConfig engine.RenderConfig
}

func parseAndPrepare(req models.JobRequestV1) (preparedRequest, *models.JobErrorV1) {
	if strings.TrimSpace(req.Mode) != "single" {
		return preparedRequest{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_MODE_INVALID", "mode must be single", nil)
	}

	// NOTE(stage-5): Batch execution intentionally out of scope for Stage 4.
	// TODO(stage-5): add batch mode for per-file markdown -> pdf jobs.

	if len(req.InputPaths) != 1 {
		return preparedRequest{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_COUNT_INVALID", "single mode requires exactly one markdown input", nil)
	}

	inputPath := strings.TrimSpace(req.InputPaths[0])
	if inputPath == "" {
		return preparedRequest{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_INPUT_REQUIRED", "inputPath is required", nil)
	}

	if err := validateInputPath(inputPath); err != nil {
		return preparedRequest{}, err
	}

	header, err := parseHeaderFooterOption(req.Options, "header")
	if err != nil {
		return preparedRequest{}, err
	}

	footer, err := parseHeaderFooterOption(req.Options, "footer")
	if err != nil {
		return preparedRequest{}, err
	}

	outputPath, outputErr := resolveOutputPath(inputPath, req.OutputDir, optionString(req.Options, "outputPath"))
	if outputErr != nil {
		return preparedRequest{}, outputErr
	}

	if sameFile(inputPath, outputPath) {
		return preparedRequest{}, models.NewCanonicalJobError("DOC_MD_TO_PDF_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	return preparedRequest{
		inputPath:  inputPath,
		outputPath: outputPath,
		renderConfig: engine.RenderConfig{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Header:     header,
			Footer:     footer,
		},
	}, nil
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

// mdEffectiveOptions summarizes the resolved header/footer configuration. It
// stays empty when the request did not configure either of them.
func mdEffectiveOptions(options map[string]any, cfg engine.RenderConfig) map[string]any {
	effective := map[string]any{}

	if raw, ok := options["header"]; ok && raw != nil {
		effective["header"] = headerFooterSummary(cfg.Header)
	}
	if raw, ok := options["footer"]; ok && raw != nil {
		effective["footer"] = headerFooterSummary(cfg.Footer)
	}

	if len(effective) == 0 {
		return nil
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

func resolveOutputPath(inputPath, outputDir, rawOutputPath string) (string, *models.JobErrorV1) {
	if strings.TrimSpace(rawOutputPath) != "" {
		outputPath := strings.TrimSpace(rawOutputPath)
		if strings.ToLower(filepath.Ext(outputPath)) != ".pdf" {
			return "", models.NewCanonicalJobError("DOC_MD_TO_PDF_OUTPUT_INVALID", "outputPath must use .pdf extension", nil)
		}
		if dirErr := validateOutputDir(filepath.Dir(outputPath)); dirErr != nil {
			return "", dirErr
		}
		if isOutputAvailable(outputPath) {
			return outputPath, nil
		}

		base := strings.TrimSuffix(outputPath, filepath.Ext(outputPath))
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s-%d.pdf", base, i)
			if isOutputAvailable(candidate) {
				return candidate, nil
			}
		}
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

	prefix := filepath.Join(targetDir, fmt.Sprintf("%s_md2pdf", base))
	first := prefix + ".pdf"
	if isOutputAvailable(first) {
		return first, nil
	}

	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d.pdf", prefix, i)
		if isOutputAvailable(candidate) {
			return candidate, nil
		}
	}
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

func isOutputAvailable(outputPath string) bool {
	_, err := os.Stat(outputPath)
	return err != nil
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

func sameFile(inputPath, outputPath string) bool {
	if strings.TrimSpace(inputPath) == "" || strings.TrimSpace(outputPath) == "" {
		return false
	}
	left := filepath.Clean(inputPath)
	right := filepath.Clean(outputPath)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func firstPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return strings.TrimSpace(paths[0])
}
