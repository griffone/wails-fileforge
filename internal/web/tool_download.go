package web

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/web/engine"
)

const (
	ToolIDWebDownload = "tool.web.download"

	webOutputKindFile = "file"

	// webDownloadStage is the single progress stage the drawer shows for a
	// download; the engine reports per-file percentages inside it.
	webDownloadStage = "web.download"

	// webDownloadProgressTotal turns a percentage into the job progress
	// contract, which only carries a current/total pair.
	webDownloadProgressTotal = 100

	// maxDownloadTitleRunes caps the readable half of an output name so long
	// upload titles never hit the filesystem limit.
	maxDownloadTitleRunes = 120
)

// Planner detail codes. They are stable because the retry policy and the UI
// branch on them, so every one of them is classified in
// models.canonicalWebCodes.
const (
	detailWebItemsRequired  = "WEB_DOWNLOAD_ITEMS_REQUIRED"
	detailWebItemsMismatch  = "WEB_DOWNLOAD_ITEMS_MISMATCH"
	detailWebOptionsInvalid = "WEB_DOWNLOAD_OPTIONS_INVALID"
)

// webLookPath resolves the runtime binaries the tool needs beyond yt-dlp. It is
// a package variable so tests can simulate a machine without them.
var webLookPath = exec.LookPath

// DownloadTool downloads media from one or more links with yt-dlp. The link
// itself is the job input; quality and naming come from the probed item list
// the caller sends in options.items.
type DownloadTool struct {
	probe  engine.RuntimeProbe
	runner engine.CommandRunner
}

func NewDownloadTool() *DownloadTool {
	return NewDownloadToolWithDeps(nil, nil)
}

func NewDownloadToolWithDeps(probe engine.RuntimeProbe, runner engine.CommandRunner) *DownloadTool {
	return &DownloadTool{probe: probe, runner: runner}
}

func (t *DownloadTool) ID() string {
	return ToolIDWebDownload
}

func (t *DownloadTool) Capability() string {
	return ToolIDWebDownload
}

func (t *DownloadTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Download Media",
		Description:      "Download a link as an MP4 video or an MP3 audio track",
		Domain:           "web",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"url"},
		OutputExtensions: []string{"mp4", "mp3"},
		RuntimeDeps:      []string{"yt-dlp", "ffmpeg", "deno"},
		ResourceClass:    "external",
		MaxWorkers:       2,
		Tags:             []string{"web", "download", "youtube"},
	}
}

func (t *DownloadTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	if err := resolveWebProbe(t.probe).Check(ctx); err != nil {
		return models.ToolRuntimeState{Status: "degraded", Healthy: false, Reason: webRuntimeReason(err)}
	}

	if missingWebBinary("ffmpeg") {
		return models.ToolRuntimeState{
			Status:  "degraded",
			Healthy: false,
			Reason:  "ffmpeg is not available, so merged video and MP3 conversion cannot run. Install / repair the toolchain from the toolchain panel.",
		}
	}

	if missingWebBinary("deno") {
		return models.ToolRuntimeState{
			Status:  "degraded",
			Healthy: false,
			Reason:  "Deno is not installed, so YouTube returns reduced formats until Deno is installed from the toolchain panel.",
		}
	}

	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *DownloadTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)

	return planErr
}

// Plan resolves the exact output file of every link without running yt-dlp and
// without writing anything. Each output is <sanitised title> [<id>].<target
// extension>, unique inside outputDir, so the path yt-dlp writes is the path the
// caller was shown.
func (t *DownloadTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	if runtimeErr := t.checkRuntime(ctx); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
	}

	if len(req.InputPaths) == 0 {
		return planFailure(plan, webValidationError(detailWebItemsRequired, "at least 1 link is required", nil))
	}

	mode := "batch"
	if len(req.InputPaths) == 1 {
		mode = "single"
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		return planFailure(plan, webValidationError(detailWebOptionsInvalid, "options.outputDir is required", nil))
	}

	kind, maxHeight, audioBitrateKbps, optionsErr := resolveDownloadOptions(req.Options)
	if optionsErr != nil {
		return planFailure(plan, optionsErr)
	}

	explicitOutput := ""
	if mode == "single" {
		if value := optionString(req.Options, "outputPath"); value != "" {
			explicitOutput = filepath.Clean(value)
		}
	} else if optionString(req.Options, "outputPath") != "" {
		return planFailure(plan, webValidationError(detailWebOptionsInvalid, "outputPath is only valid for a single link; a batch uses the output directory", nil))
	}

	items, itemsErr := downloadItemsFromOptions(req.Options)
	if itemsErr != nil {
		return planFailure(plan, itemsErr)
	}
	if len(items) == 0 {
		return planFailure(plan, webValidationError(
			detailWebItemsRequired,
			"options.items is required; probe each link first so the download knows its title and id",
			nil,
		))
	}
	if len(items) != len(req.InputPaths) {
		return planFailure(plan, webValidationError(
			detailWebItemsMismatch,
			fmt.Sprintf("options.items has %d entries but the request has %d links; probe the link first", len(items), len(req.InputPaths)),
			map[string]any{"items": len(items), "links": len(req.InputPaths)},
		))
	}

	outputs := make([]models.PlannedOutput, 0, len(req.InputPaths))
	warnings := make([]string, 0, len(req.InputPaths))
	used := make(map[string]struct{}, len(items))

	for index, item := range items {
		url := strings.TrimSpace(req.InputPaths[index])
		if urlErr := mapWebError(engine.ValidateURL(url)); urlErr != nil {
			return planFailure(plan, urlErr)
		}

		if item.url != url {
			return planFailure(plan, webValidationError(
				detailWebItemsMismatch,
				fmt.Sprintf("options.items[%d].url does not match inputPaths[%d]; probe the link first", index, index),
				map[string]any{"itemIndex": index},
			))
		}

		planned := models.PlannedOutput{
			InputPath:  url,
			OutputKind: webOutputKindFile,
		}

		if explicitOutput != "" {
			planned.OutputPath = explicitOutput
			if _, statErr := os.Stat(planned.OutputPath); statErr == nil {
				planned.Exists = true
			}
		} else {
			base := downloadOutputBase(item.title, item.id)
			outputPath, adjusted := outputplan.UniqueFilePath(outputDir, base, downloadOutputExtension(kind), used)
			planned.OutputPath = outputPath
			if adjusted {
				planned.Adjusted = true
				planned.Warnings = append(planned.Warnings, fmt.Sprintf(
					"%s already exists. Saving as %s.",
					filepath.Base(filepath.Join(outputDir, base+"."+downloadOutputExtension(kind))),
					filepath.Base(outputPath),
				))
			}
		}

		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	resolvedItems := make([]map[string]any, 0, len(outputs))
	for _, planned := range outputs {
		resolvedItems = append(resolvedItems, map[string]any{
			"url":              planned.InputPath,
			"outputPath":       planned.OutputPath,
			"kind":             kind,
			"maxHeight":        maxHeight,
			"audioBitrateKbps": audioBitrateKbps,
		})
	}

	plan.Success = true
	plan.Message = fmt.Sprintf("Ready to download %d %s.", len(outputs), pluralizeWeb("link", len(outputs)))
	plan.Outputs = outputs
	plan.Warnings = warnings
	effectiveOptions := map[string]any{
		"kind":  kind,
		"items": resolvedItems,
	}
	if kind == engine.KindAudio {
		effectiveOptions["audioBitrateKbps"] = audioBitrateKbps
	} else {
		effectiveOptions["maxHeight"] = maxHeight
	}
	plan.EffectiveOptions = effectiveOptions

	return plan, nil
}

func (t *DownloadTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *DownloadTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return webPlanFailureItem(firstWebInput(req.InputPaths), firstWebPlannedOutput(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError(engine.CodeExecutionFailed, "web download execution returned no result", nil)
	}

	return webPlanFailureItem(firstWebInput(req.InputPaths), firstWebPlannedOutput(plan), jobErr), jobErr
}

func (t *DownloadTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan downloads exactly the outputs resolved by Plan. Per-item failures
// are isolated, the first one is returned for the job state, and cancellation
// stops immediately. Batch progress is one continuous bar across all links.
func (t *DownloadTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	items := make([]models.JobResultItem, 0, len(plan.Outputs))
	resolved := effectiveDownloadItems(plan.EffectiveOptions)
	resolvedByPath := make(map[string]resolvedDownloadItem, len(resolved))
	for _, item := range resolved {
		resolvedByPath[item.outputPath] = item
	}

	defaultKind := optionString(plan.EffectiveOptions, "kind")
	if defaultKind == "" {
		defaultKind = engine.KindVideo
	}
	defaultMaxHeight := webOptionIntValue(plan.EffectiveOptions, "maxHeight")
	defaultBitrate := webOptionIntValue(plan.EffectiveOptions, "audioBitrateKbps")

	total := len(plan.Outputs)
	var firstErr *models.JobError

	for index, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			return items, canceledDownloadError()
		default:
		}

		item, found := resolvedByPath[planned.OutputPath]
		if !found {
			item = resolvedDownloadItem{
				url:              planned.InputPath,
				outputPath:       planned.OutputPath,
				kind:             defaultKind,
				maxHeight:        defaultMaxHeight,
				audioBitrateKbps: defaultBitrate,
			}
		}

		downloadRequest := engine.Request{
			URL:              item.url,
			OutputDir:        filepath.Dir(planned.OutputPath),
			OutputPath:       planned.OutputPath,
			Kind:             item.kind,
			MaxHeight:        item.maxHeight,
			AudioBitrateKbps: item.audioBitrateKbps,
		}

		lastPercent := 0
		itemProgress := func(progress engine.Progress) {
			if onProgress == nil {
				return
			}
			percent := jobPercent(progress.Percent)
			if percent < lastPercent {
				percent = lastPercent
			}
			lastPercent = percent
			overall := overallDownloadPercent(index, total, percent)
			onProgress(models.JobProgress{
				Current: overall,
				Total:   webDownloadProgressTotal,
				Stage:   webDownloadStage,
				Message: downloadProgressMessage(index, total, overall),
			})
		}

		producedPath, downloadErr := engine.Download(ctx, t.runner, downloadRequest, itemProgress)
		if downloadErr != nil {
			jobErr := mapWebError(downloadErr)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, webPlanFailureItem(planned.InputPath, planned.OutputPath, jobErr))
			continue
		}

		outputPath := strings.TrimSpace(producedPath)
		if outputPath == "" {
			outputPath = planned.OutputPath
		}
		items = append(items, models.JobResultItem{
			InputPath:   planned.InputPath,
			OutputPath:  outputPath,
			Outputs:     []string{outputPath},
			OutputCount: 1,
			Success:     true,
			Message:     "Download successful",
		})
		if onProgress != nil {
			overall := overallDownloadPercent(index, total, webDownloadProgressTotal)
			onProgress(models.JobProgress{
				Current: overall,
				Total:   webDownloadProgressTotal,
				Stage:   webDownloadStage,
				Message: downloadProgressMessage(index, total, overall),
			})
		}
	}

	return items, firstErr
}

// checkRuntime fails the plan when the downloader or the media toolchain it
// needs to produce the planned file is missing. A missing Deno only reduces the
// formats YouTube offers, so it degrades the manifest instead of blocking.
func (t *DownloadTool) checkRuntime(ctx context.Context) *models.JobError {
	if err := resolveWebProbe(t.probe).Check(ctx); err != nil {
		var webErr *engine.WebError
		if errors.As(err, &webErr) {
			return mapWebError(webErr)
		}

		return models.NewCanonicalJobError(engine.CodeRuntimeUnavailable, err.Error(), nil)
	}

	if missingWebBinary("ffmpeg") {
		return models.NewCanonicalJobError(
			engine.CodeRuntimeUnavailable,
			"ffmpeg is not available, so the download cannot be merged or converted. Install / repair the toolchain from the toolchain panel.",
			map[string]any{"binary": "ffmpeg"},
		)
	}

	return nil
}

// downloadItem is one probed queue item. The output container follows kind, so
// the probed ext is accepted for contract completeness and never used for
// naming.
type downloadItem struct {
	url   string
	title string
	id    string
}

// downloadItemsFromOptions reads the probe result list the caller sends. Each
// entry carries url, title, id and ext; ext is accepted and ignored because the
// planned container follows kind, not the probed source format.
func downloadItemsFromOptions(options map[string]any) ([]downloadItem, *models.JobError) {
	if options == nil {
		return nil, nil
	}

	raw, exists := options["items"]
	if !exists || raw == nil {
		return nil, nil
	}

	switch entries := raw.(type) {
	case []downloadItem:
		return append([]downloadItem(nil), entries...), nil
	case []map[string]any:
		return downloadItemsFromMaps(entries)
	case []any:
		return downloadItemsFromAny(entries)
	default:
		return nil, webValidationError(detailWebItemsMismatch, "options.items must be an array of probe results; probe the link first", nil)
	}
}

func downloadItemsFromMaps(entries []map[string]any) ([]downloadItem, *models.JobError) {
	items := make([]downloadItem, 0, len(entries))
	for index, entry := range entries {
		item, itemErr := downloadItemFromMap(entry, index)
		if itemErr != nil {
			return nil, itemErr
		}
		items = append(items, item)
	}

	return items, nil
}

func downloadItemsFromAny(entries []any) ([]downloadItem, *models.JobError) {
	items := make([]downloadItem, 0, len(entries))
	for index, entry := range entries {
		values, ok := entry.(map[string]any)
		if !ok {
			return nil, webValidationError(
				detailWebItemsMismatch,
				fmt.Sprintf("options.items[%d] must be an object with url, title, id and ext; probe the link first", index),
				map[string]any{"itemIndex": index},
			)
		}

		item, itemErr := downloadItemFromMap(values, index)
		if itemErr != nil {
			return nil, itemErr
		}
		items = append(items, item)
	}

	return items, nil
}

func downloadItemFromMap(values map[string]any, index int) (downloadItem, *models.JobError) {
	url := strings.TrimSpace(webStringField(values, "url"))
	if url == "" {
		return downloadItem{}, webValidationError(
			detailWebItemsMismatch,
			fmt.Sprintf("options.items[%d].url is required; probe the link first", index),
			map[string]any{"itemIndex": index},
		)
	}

	return downloadItem{
		url:   url,
		title: webStringField(values, "title"),
		id:    webStringField(values, "id"),
	}, nil
}

// resolveDownloadOptions applies the shared quality defaults. The caller can
// rely on these values being present in EffectiveOptions even when the request
// omits them.
func resolveDownloadOptions(options map[string]any) (string, int, int, *models.JobError) {
	kind := strings.ToLower(optionString(options, "kind"))
	switch kind {
	case "":
		kind = engine.KindVideo
	case engine.KindVideo, engine.KindAudio:
	default:
		return "", 0, 0, webValidationError(detailWebOptionsInvalid, "kind must be video or audio", map[string]any{"kind": optionString(options, "kind")})
	}

	maxHeight, heightErr := webOptionInt(options, "maxHeight")
	if heightErr != nil {
		return "", 0, 0, heightErr
	}
	if maxHeight < 0 {
		return "", 0, 0, webValidationError(detailWebOptionsInvalid, "maxHeight must be >= 0", map[string]any{"maxHeight": maxHeight})
	}

	audioBitrateKbps, bitrateErr := webOptionInt(options, "audioBitrateKbps")
	if bitrateErr != nil {
		return "", 0, 0, bitrateErr
	}
	if audioBitrateKbps < 0 {
		return "", 0, 0, webValidationError(detailWebOptionsInvalid, "audioBitrateKbps must be >= 0", map[string]any{"audioBitrateKbps": audioBitrateKbps})
	}

	return kind, maxHeight, audioBitrateKbps, nil
}

// resolvedDownloadItem is the executable form of one planned output.
type resolvedDownloadItem struct {
	url              string
	outputPath       string
	kind             string
	maxHeight        int
	audioBitrateKbps int
}

// effectiveDownloadItems reads the resolved items back from the plan. Both the
// programmatic shape ([]resolvedDownloadItem, []map[string]any) and the JSON
// shape ([]any of map[string]any) are accepted.
func effectiveDownloadItems(options map[string]any) []resolvedDownloadItem {
	if options == nil {
		return nil
	}

	switch entries := options["items"].(type) {
	case []resolvedDownloadItem:
		return append([]resolvedDownloadItem(nil), entries...)
	case []map[string]any:
		return resolvedDownloadItemsFromMaps(entries)
	case []any:
		items := make([]resolvedDownloadItem, 0, len(entries))
		for _, entry := range entries {
			if values, ok := entry.(map[string]any); ok {
				items = append(items, resolvedDownloadItemFromMap(values))
			}
		}
		return items
	default:
		return nil
	}
}

func resolvedDownloadItemsFromMaps(entries []map[string]any) []resolvedDownloadItem {
	items := make([]resolvedDownloadItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, resolvedDownloadItemFromMap(entry))
	}

	return items
}

func resolvedDownloadItemFromMap(values map[string]any) resolvedDownloadItem {
	return resolvedDownloadItem{
		url:              webStringField(values, "url"),
		outputPath:       webStringField(values, "outputPath"),
		kind:             strings.ToLower(webStringField(values, "kind")),
		maxHeight:        webIntField(values, "maxHeight"),
		audioBitrateKbps: webIntField(values, "audioBitrateKbps"),
	}
}

// mapWebError translates an engine failure into the tool contract. The canonical
// family comes from models.canonicalWebCodes, so a missing binary lands in the
// runtime family while the coarse WEB_DOWNLOAD_* code stays as DetailCode for
// the retry policy and the UI.
func mapWebError(webErr *engine.WebError) *models.JobError {
	if webErr == nil {
		return nil
	}

	detailCode := strings.TrimSpace(webErr.Code)
	if detailCode == "" {
		detailCode = engine.CodeExecutionFailed
	}

	return models.NewCanonicalJobError(detailCode, webErr.Message, webErr.Details)
}

func webValidationError(detailCode, message string, details map[string]any) *models.JobError {
	return models.NewCanonicalJobError(detailCode, message, details)
}

// sanitizeDownloadTitle replaces path separators and control characters with a
// space, collapses whitespace and caps the length so a hostile or noisy title
// can never escape the output directory.
func sanitizeDownloadTitle(title string) string {
	var builder strings.Builder
	for _, r := range title {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			builder.WriteRune(' ')
			continue
		}
		builder.WriteRune(r)
	}

	collapsed := strings.Join(strings.Fields(builder.String()), " ")
	runes := []rune(collapsed)
	if len(runes) > maxDownloadTitleRunes {
		runes = runes[:maxDownloadTitleRunes]
	}

	trimmed := strings.TrimSpace(string(runes))
	if trimmed == "" {
		return "download"
	}

	return trimmed
}

func sanitizeDownloadID(id string) string {
	var builder strings.Builder
	for _, r := range id {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			builder.WriteRune(' ')
			continue
		}
		builder.WriteRune(r)
	}

	return strings.Join(strings.Fields(builder.String()), " ")
}

func downloadOutputBase(title, id string) string {
	base := sanitizeDownloadTitle(title)
	if sanitizedID := sanitizeDownloadID(id); sanitizedID != "" {
		return base + " [" + sanitizedID + "]"
	}

	return base
}

func downloadOutputExtension(kind string) string {
	if kind == engine.KindAudio {
		return "mp3"
	}

	return "mp4"
}

func missingWebBinary(name string) bool {
	lookup := webLookPath
	if lookup == nil {
		lookup = exec.LookPath
	}

	_, err := lookup(name)
	return err != nil
}

func resolveWebProbe(probe engine.RuntimeProbe) engine.RuntimeProbe {
	if probe != nil {
		return probe
	}

	return engine.NewToolRuntimeProbe()
}

func webRuntimeReason(err error) string {
	var webErr *engine.WebError
	if errors.As(err, &webErr) && webErr != nil && strings.TrimSpace(webErr.Message) != "" {
		return webErr.Message
	}

	return err.Error()
}

func canceledDownloadError() *models.JobError {
	return models.NewCanonicalJobError(engine.CodeCanceled, "the download was canceled", nil)
}

func overallDownloadPercent(index, total, itemPercent int) int {
	if total <= 0 {
		return 0
	}

	return jobPercent((float64(index)*100 + float64(itemPercent)) / float64(total))
}

func jobPercent(percent float64) int {
	if math.IsNaN(percent) || percent <= 0 {
		return 0
	}
	if percent >= 100 {
		return webDownloadProgressTotal
	}

	return int(math.Round(percent))
}

func downloadProgressMessage(index, total, percent int) string {
	if total <= 1 {
		return fmt.Sprintf("Downloading… %d%%", percent)
	}

	return fmt.Sprintf("Downloading item %d of %d… %d%%", index+1, total, percent)
}

func planFailure(plan models.PlanJobResponse, jobErr *models.JobError) (models.PlanJobResponse, *models.JobError) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}

	return plan, jobErr
}

func webPlanFailureItem(inputPath, outputPath string, jobErr *models.JobError) models.JobResultItem {
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

func firstWebInput(inputPaths []string) string {
	if len(inputPaths) == 0 {
		return ""
	}

	return strings.TrimSpace(inputPaths[0])
}

func firstWebPlannedOutput(plan models.PlanJobResponse) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func optionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}

	value, ok := options[key].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}

// webOptionInt reads an optional integer option. A supplied value of the wrong
// type or a fractional number is rejected instead of being truncated.
func webOptionInt(options map[string]any, key string) (int, *models.JobError) {
	if options == nil {
		return 0, nil
	}

	raw, exists := options[key]
	if !exists || raw == nil {
		return 0, nil
	}

	if floatValue, isFloat := raw.(float64); isFloat && floatValue != float64(int(floatValue)) {
		return 0, webValidationError(detailWebOptionsInvalid, fmt.Sprintf("options.%s must be an integer", key), map[string]any{"option": key})
	}

	switch value := raw.(type) {
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
		return 0, webValidationError(detailWebOptionsInvalid, fmt.Sprintf("options.%s must be numeric", key), map[string]any{"option": key})
	}
}

func webOptionIntValue(options map[string]any, key string) int {
	value, err := webOptionInt(options, key)
	if err != nil {
		return 0
	}

	return value
}

func webStringField(values map[string]any, key string) string {
	value, ok := values[key].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}

func webIntField(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case float64:
		return int(value)
	case float32:
		return int(value)
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	default:
		return 0
	}
}

func pluralizeWeb(word string, count int) string {
	if count == 1 {
		return word
	}

	return word + "s"
}
