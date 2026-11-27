package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/tooloptions"
)

const (
	// maxInputsPerCall bounds the work one tools/call can request. It is
	// documented in every input schema and enforced before planning.
	maxInputsPerCall = 50

	// recipePollInterval matches the headless CLI: the recipe service exposes a
	// snapshot instead of a wait channel.
	recipePollInterval = 20 * time.Millisecond
)

// exposedToolPrefixes is the allowlist of catalog domains MCP may expose. Every
// tool in these domains writes a new output and never moves or deletes an
// existing file. Anything else, including automation, organize, duplicate
// scans, and any future file-manipulation domain, is hidden from tools/list and
// refused by run_job.
var exposedToolPrefixes = []string{
	"tool.audio.",
	"tool.doc.",
	"tool.image.",
	"tool.pdf.",
	"tool.video.",
}

// pathOptionKeys are option keys that carry a filesystem path. Input-side paths
// use the input permission code; output-side paths use the output code.
var pathOptionKeys = []string{"logoPath", "modelPath", "outputDir", "outputPath", "whisperPath"}

// outputPathOptionKeys are the pathOptionKeys that name a destination.
var outputPathOptionKeys = map[string]struct{}{
	"outputDir":  {},
	"outputPath": {},
}

type toolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
	Annotations map[string]any
	Handler     func(context.Context, json.RawMessage) (any, bool)
}

func (s *Server) buildToolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name: "list_tools",
			Description: "List the FileForge catalog: tool id, name, description, input and output extensions, " +
				"whether the tool supports batch, and the option keys it accepts. Read-only.",
			InputSchema: emptyObjectSchema(),
			Annotations: readOnlyAnnotations(),
			Handler:     s.handleListTools,
		},
		{
			Name: "plan_job",
			Description: fmt.Sprintf(
				"Validate a job and return its planned output paths and warnings without writing anything. "+
					"Every input path, output directory and path-valued option must resolve inside a folder granted "+
					"with `%s`. An empty allowlist denies everything. At most %d inputs per call.",
				grantCommand, maxInputsPerCall,
			),
			InputSchema: jobInputSchema("toolId", "FileForge tool id, for example tool.pdf.merge."),
			Annotations: readOnlyAnnotations(),
			Handler:     s.handlePlanJob,
		},
		{
			Name: "run_job",
			Description: fmt.Sprintf(
				"Plan and run a FileForge job, then return its terminal status, output paths and canonical error. "+
					"Every path must resolve inside a folder granted with `%s`. This server cannot add, remove or list "+
					"grants; only the terminal command can. At most %d inputs per call.",
				grantCommand, maxInputsPerCall,
			),
			InputSchema: jobInputSchema("toolId", "FileForge tool id, for example tool.pdf.merge."),
			Annotations: sideEffectAnnotations(),
			Handler:     s.handleRunJob,
		},
		{
			Name:        "list_recipes",
			Description: "List the FileForge recipe catalog: id, name, description, accepted inputs and options. Read-only.",
			InputSchema: emptyObjectSchema(),
			Annotations: readOnlyAnnotations(),
			Handler:     s.handleListRecipes,
		},
		{
			Name: "plan_recipe",
			Description: fmt.Sprintf(
				"Validate a recipe and return its planned output paths and warnings without running any step. "+
					"Every input path and output directory must resolve inside a folder granted with `%s`. "+
					"At most %d inputs per call.",
				grantCommand, maxInputsPerCall,
			),
			InputSchema: jobInputSchema("recipeId", "FileForge recipe id, for example recipe.email_pdf."),
			Annotations: readOnlyAnnotations(),
			Handler:     s.handlePlanRecipe,
		},
		{
			Name: "run_recipe",
			Description: fmt.Sprintf(
				"Plan and run a FileForge recipe, then return its terminal status, output paths and canonical error. "+
					"Every path must resolve inside a folder granted with `%s`. This server cannot change grants. "+
					"At most %d inputs per call.",
				grantCommand, maxInputsPerCall,
			),
			InputSchema: jobInputSchema("recipeId", "FileForge recipe id, for example recipe.email_pdf."),
			Annotations: sideEffectAnnotations(),
			Handler:     s.handleRunRecipe,
		},
	}
}

func emptyObjectSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func jobInputSchema(idKey, idDescription string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			idKey: map[string]any{
				"type":        "string",
				"description": idDescription,
			},
			"inputPaths": map[string]any{
				"type": "array",
				"description": fmt.Sprintf(
					"Absolute input paths. Each path must resolve inside a folder granted with `%s`. At most %d paths per call.",
					grantCommand, maxInputsPerCall,
				),
				"items":    map[string]any{"type": "string"},
				"minItems": 1,
				"maxItems": maxInputsPerCall,
			},
			"outputDir": map[string]any{
				"type": "string",
				"description": fmt.Sprintf(
					"Optional absolute output directory. It must resolve inside a granted folder and defaults to the input folder. Grant one with `%s`.",
					grantCommand,
				),
			},
			"options": map[string]any{
				"type":                 "object",
				"description":          "Tool or recipe options. Keys the target does not read are rejected. Path-valued options must also resolve inside a granted folder.",
				"additionalProperties": true,
			},
		},
		"required":             []string{idKey, "inputPaths"},
		"additionalProperties": false,
	}
}

func readOnlyAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    true,
		"destructiveHint": false,
		"idempotentHint":  false,
		"openWorldHint":   false,
	}
}

func sideEffectAnnotations() map[string]any {
	return map[string]any{
		"readOnlyHint":    false,
		"destructiveHint": false,
		"idempotentHint":  false,
		"openWorldHint":   false,
	}
}

// catalogEntry is one row of the list_tools payload.
type catalogEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Inputs      []string `json:"inputs"`
	Outputs     []string `json:"outputs"`
	Batch       bool     `json:"batch"`
	Options     []string `json:"options"`
	Enabled     bool     `json:"enabled"`
}

type listToolsPayload struct {
	Tools []catalogEntry `json:"tools"`
}

func (s *Server) handleListTools(ctx context.Context, _ json.RawMessage) (any, bool) {
	if s.registry == nil || s.tooling == nil {
		return listToolsPayload{Tools: []catalogEntry{}}, false
	}

	response := s.tooling.ListTools()
	entries := make([]catalogEntry, 0, len(response.Tools))
	for _, entry := range response.Tools {
		if isExcludedTool(entry.Manifest.ToolID) {
			continue
		}
		options, _ := tooloptions.KnownKeys(entry.Manifest.ToolID)
		entries = append(entries, catalogEntry{
			ID:          entry.Manifest.ToolID,
			Name:        entry.Manifest.Name,
			Description: entry.Manifest.Description,
			Inputs:      nonNilStrings(entry.Manifest.InputExtensions),
			Outputs:     nonNilStrings(entry.Manifest.OutputExtensions),
			Batch:       entry.Manifest.SupportsBatch,
			Options:     nonNilStrings(options),
			Enabled:     entry.State.Status == "enabled",
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	return listToolsPayload{Tools: entries}, false
}

func isExcludedTool(toolID string) bool {
	for _, prefix := range exposedToolPrefixes {
		if strings.HasPrefix(toolID, prefix) {
			return false
		}
	}

	return true
}

// recipeEntry is one row of the list_recipes payload.
type recipeEntry struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	Description     string                     `json:"description"`
	Category        string                     `json:"category"`
	InputExtensions []string                   `json:"inputExtensions"`
	MaxInputs       int                        `json:"maxInputs"`
	Options         []models.RecipeOptionField `json:"options"`
	UserDefined     bool                       `json:"userDefined"`
}

type listRecipesPayload struct {
	Recipes []recipeEntry `json:"recipes"`
}

func (s *Server) handleListRecipes(_ context.Context, _ json.RawMessage) (any, bool) {
	if s.recipes == nil {
		return listRecipesPayload{Recipes: []recipeEntry{}}, false
	}

	list := s.recipes.List()
	entries := make([]recipeEntry, 0, len(list))
	for _, recipe := range list {
		options := recipe.OptionSchema
		if options == nil {
			options = []models.RecipeOptionField{}
		}
		entries = append(entries, recipeEntry{
			ID:              recipe.ID,
			Name:            recipe.Name,
			Description:     recipe.Description,
			Category:        recipe.Category,
			InputExtensions: nonNilStrings(recipe.AcceptExtensions),
			MaxInputs:       recipe.MaxInputs,
			Options:         options,
			UserDefined:     recipe.UserDefined,
		})
	}

	return listRecipesPayload{Recipes: entries}, false
}

// jobArguments is the wire shape shared by plan_job and run_job.
type jobArguments struct {
	ID         string         `json:"-"`
	InputPaths []string       `json:"inputPaths"`
	OutputDir  string         `json:"outputDir"`
	Options    map[string]any `json:"options"`
}

// decodeJobArguments decodes arguments for a job or recipe call. The id field
// name differs between the two schemas, so the caller identifies it.
func decodeJobArguments(raw json.RawMessage, idKey string) (jobArguments, *models.JobError) {
	fields := make(map[string]json.RawMessage)
	if err := decodeStrict(raw, &fields); err != nil {
		return jobArguments{}, argumentError(err)
	}

	allowed := map[string]struct{}{idKey: {}, "inputPaths": {}, "outputDir": {}, "options": {}}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return jobArguments{}, argumentError(fmt.Errorf("unknown argument %q", key))
		}
	}

	var args jobArguments
	if rawID, ok := fields[idKey]; ok {
		if err := json.Unmarshal(rawID, &args.ID); err != nil {
			return jobArguments{}, argumentError(fmt.Errorf("%s must be a string", idKey))
		}
	}
	if rawInputs, ok := fields["inputPaths"]; ok {
		if err := json.Unmarshal(rawInputs, &args.InputPaths); err != nil {
			return jobArguments{}, argumentError(errors.New("inputPaths must be an array of strings"))
		}
	}
	if rawOutputDir, ok := fields["outputDir"]; ok {
		if err := json.Unmarshal(rawOutputDir, &args.OutputDir); err != nil {
			return jobArguments{}, argumentError(errors.New("outputDir must be a string"))
		}
	}
	if rawOptions, ok := fields["options"]; ok {
		if err := json.Unmarshal(rawOptions, &args.Options); err != nil {
			return jobArguments{}, argumentError(errors.New("options must be an object"))
		}
	}

	return args, nil
}

func decodeStrict(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("arguments contain trailing data")
	}

	return nil
}

func argumentError(err error) *models.JobError {
	return models.NewCanonicalJobError("VALIDATION_ERROR", fmt.Sprintf("invalid arguments: %v", err), nil)
}

// validateCallShape enforces the per-call limits shared by jobs and recipes.
func validateCallShape(id, idKey string, inputs []string) *models.JobError {
	if strings.TrimSpace(id) == "" {
		return argumentError(fmt.Errorf("%s is required", idKey))
	}
	if len(inputs) == 0 {
		return argumentError(errors.New("inputPaths must contain at least one path"))
	}
	if len(inputs) > maxInputsPerCall {
		return models.NewCanonicalJobError(
			"MCP_INPUT_LIMIT_EXCEEDED",
			fmt.Sprintf("At most %d input paths can be processed per call; %d were supplied. Split the work into several calls.", maxInputsPerCall, len(inputs)),
			map[string]any{"limit": maxInputsPerCall, "supplied": len(inputs)},
		)
	}
	for _, input := range inputs {
		if strings.TrimSpace(input) == "" {
			return argumentError(errors.New("inputPaths must not contain an empty path"))
		}
	}

	return nil
}

// prepareJob resolves the tool, rejects unread option keys and checks every
// request path. denied is true only for a permission failure, which the audit
// log records separately from a plain validation rejection.
func (s *Server) prepareJob(args jobArguments) (mode string, jobErr *models.JobError, denied bool) {
	if err := validateCallShape(args.ID, "toolId", args.InputPaths); err != nil {
		return "", err, false
	}
	if isExcludedTool(args.ID) {
		return "", models.NewCanonicalJobError(
			"TOOL_NOT_FOUND",
			fmt.Sprintf("Tool '%s' is not available over MCP. Exposed domains: tool.audio, tool.doc, tool.image, tool.pdf, tool.video.", args.ID),
			map[string]any{"toolId": args.ID},
		), false
	}
	if s.registry == nil || s.tooling == nil {
		return "", models.NewCanonicalJobError("TOOL_NOT_FOUND", fmt.Sprintf("Tool '%s' is not available in this process.", args.ID), map[string]any{"toolId": args.ID}), false
	}

	tool, err := s.registry.GetTool(args.ID)
	if err != nil {
		return "", models.NewCanonicalJobError("TOOL_NOT_FOUND", err.Error(), map[string]any{"toolId": args.ID}), false
	}

	if unknown, keys, found := tooloptions.UnknownKey(args.ID, args.Options); found && unknown != "" {
		message := fmt.Sprintf("Option '%s' is not supported by tool '%s'. Supported options: %s.", unknown, args.ID, strings.Join(keys, ", "))
		return "", models.NewCanonicalJobError("TOOL_OPTION_UNKNOWN", message, map[string]any{"option": unknown, "toolId": args.ID}), false
	}

	if jobErr, denied = s.checkRequestPaths(args.InputPaths, args.OutputDir, args.Options); jobErr != nil {
		return "", jobErr, denied
	}

	mode = "single"
	if tool.Manifest().SupportsBatch && len(args.InputPaths) > 1 {
		mode = "batch"
	}

	return mode, nil, false
}

// prepareRecipe resolves the recipe, validates option keys against its schema
// and checks every request path.
func (s *Server) prepareRecipe(args jobArguments) (jobErr *models.JobError, denied bool) {
	if err := validateCallShape(args.ID, "recipeId", args.InputPaths); err != nil {
		return err, false
	}
	if s.recipes == nil {
		return models.NewCanonicalJobError("RECIPE_NOT_FOUND", fmt.Sprintf("Recipe '%s' is not available in this process.", args.ID), map[string]any{"recipeId": args.ID}), false
	}

	recipe, found := s.findRecipe(args.ID)
	if !found {
		return models.NewCanonicalJobError("RECIPE_NOT_FOUND", fmt.Sprintf("Recipe '%s' was not found.", args.ID), map[string]any{"recipeId": args.ID}), false
	}

	if recipe.UserDefined && len(args.Options) > 0 {
		return models.NewCanonicalJobError(
			"VALIDATION_ERROR",
			fmt.Sprintf("User recipe '%s' stores its options; request options are not accepted. Edit the recipe in the desktop app instead.", recipe.Name),
			map[string]any{"recipeId": recipe.ID},
		), false
	}

	if unknown, supported := unknownRecipeOption(recipe, args.Options); unknown != "" {
		message := fmt.Sprintf("Option '%s' is not supported by recipe '%s'. Supported options: %s.", unknown, recipe.Name, describeOptionKeys(supported))
		return models.NewCanonicalJobError("RECIPE_OPTION_UNKNOWN", message, map[string]any{"option": unknown, "recipeId": recipe.ID}), false
	}

	if jobErr, denied = s.checkRequestPaths(args.InputPaths, args.OutputDir, args.Options); jobErr != nil {
		return jobErr, denied
	}

	return nil, false
}

func (s *Server) findRecipe(recipeID string) (models.Recipe, bool) {
	trimmed := strings.TrimSpace(recipeID)
	for _, recipe := range s.recipes.List() {
		if recipe.ID == trimmed {
			return recipe, true
		}
	}

	return models.Recipe{}, false
}

func unknownRecipeOption(recipe models.Recipe, options map[string]any) (string, []string) {
	supported := make([]string, 0, len(recipe.OptionSchema))
	allowed := make(map[string]struct{}, len(recipe.OptionSchema))
	for _, field := range recipe.OptionSchema {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		supported = append(supported, key)
		allowed[key] = struct{}{}
	}
	sort.Strings(supported)

	unknown := make([]string, 0, len(options))
	for key := range options {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return "", supported
	}
	sort.Strings(unknown)

	return unknown[0], supported
}

func describeOptionKeys(keys []string) string {
	if len(keys) == 0 {
		return "none"
	}

	return strings.Join(keys, ", ")
}

// checkRequestPaths enforces containment for inputs, the output directory and
// every path-valued option.
func (s *Server) checkRequestPaths(inputs []string, outputDir string, options map[string]any) (*models.JobError, bool) {
	policy := s.effectivePolicy()

	for _, input := range inputs {
		if err := policy.Check(input); err != nil {
			return pathJobError("MCP_PATH_NOT_ALLOWED", input, err), true
		}
	}

	if strings.TrimSpace(outputDir) != "" {
		if err := policy.Check(outputDir); err != nil {
			return pathJobError("MCP_OUTPUT_NOT_ALLOWED", outputDir, err), true
		}
	}

	for _, key := range pathOptionKeys {
		raw, present := options[key]
		if !present {
			continue
		}
		value, isString := raw.(string)
		if !isString {
			return argumentError(fmt.Errorf("option %q must be a string path", key)), false
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		code := "MCP_PATH_NOT_ALLOWED"
		if _, isOutput := outputPathOptionKeys[key]; isOutput {
			code = "MCP_OUTPUT_NOT_ALLOWED"
		}
		if err := policy.Check(value); err != nil {
			return pathJobError(code, value, err), true
		}
	}

	return nil, false
}

func (s *Server) checkPlannedOutputs(outputs []models.PlannedOutput) *models.JobError {
	policy := s.effectivePolicy()
	for _, output := range outputs {
		path := strings.TrimSpace(output.OutputPath)
		if path == "" {
			continue
		}
		if err := policy.Check(path); err != nil {
			return pathJobError("MCP_OUTPUT_NOT_ALLOWED", path, err)
		}
	}

	return nil
}

func (s *Server) checkOutputPaths(paths []string) *models.JobError {
	policy := s.effectivePolicy()
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if err := policy.Check(trimmed); err != nil {
			return pathJobError("MCP_OUTPUT_NOT_ALLOWED", trimmed, err)
		}
	}

	return nil
}

func (s *Server) effectivePolicy() *Policy {
	if s.policy == nil {
		return NewPolicy(nil)
	}

	return s.policy
}

func pathJobError(code, path string, err error) *models.JobError {
	details := map[string]any{"path": path}
	var denied *DeniedPathError
	if errors.As(err, &denied) {
		details["allowedRoots"] = denied.Roots
	}

	return models.NewCanonicalJobError(code, err.Error(), details)
}

// planJobPayload is the plan_job response.
type planJobPayload struct {
	Success          bool                   `json:"success"`
	ToolID           string                 `json:"toolId"`
	PlannedOutputs   []models.PlannedOutput `json:"plannedOutputs"`
	Warnings         []string               `json:"warnings"`
	EffectiveOptions map[string]any         `json:"effectiveOptions,omitempty"`
	Error            *models.JobError       `json:"error,omitempty"`
}

func (s *Server) handlePlanJob(_ context.Context, raw json.RawMessage) (any, bool) {
	args, jobErr := decodeJobArguments(raw, "toolId")
	if jobErr != nil {
		return planJobPayload{Success: false, PlannedOutputs: []models.PlannedOutput{}, Warnings: []string{}, Error: jobErr}, true
	}

	payload := planJobPayload{Success: false, ToolID: args.ID, PlannedOutputs: []models.PlannedOutput{}, Warnings: []string{}}

	mode, jobErr, _ := s.prepareJob(args)
	if jobErr != nil {
		payload.Error = jobErr
		return payload, true
	}

	request := models.JobRequest{
		ToolID:     args.ID,
		Mode:       mode,
		InputPaths: args.InputPaths,
		OutputDir:  args.OutputDir,
		Options:    args.Options,
	}

	plan := s.tooling.PlanJob(request)
	if !plan.Success {
		payload.Error = planError(plan)
		return payload, true
	}
	if jobErr := s.checkPlannedOutputs(plan.Outputs); jobErr != nil {
		payload.Error = jobErr
		return payload, true
	}

	payload.Success = true
	payload.PlannedOutputs = nonNilPlannedOutputs(plan.Outputs)
	payload.Warnings = nonNilStrings(plan.Warnings)
	payload.EffectiveOptions = plan.EffectiveOptions

	return payload, false
}

type planRecipePayload struct {
	Success        bool                   `json:"success"`
	RecipeID       string                 `json:"recipeId"`
	OutputDir      string                 `json:"outputDir"`
	PlannedOutputs []models.PlannedOutput `json:"plannedOutputs"`
	Warnings       []string               `json:"warnings"`
	Error          *models.JobError       `json:"error,omitempty"`
}

func (s *Server) handlePlanRecipe(_ context.Context, raw json.RawMessage) (any, bool) {
	args, jobErr := decodeJobArguments(raw, "recipeId")
	if jobErr != nil {
		return planRecipePayload{Success: false, PlannedOutputs: []models.PlannedOutput{}, Warnings: []string{}, Error: jobErr}, true
	}

	payload := planRecipePayload{Success: false, RecipeID: args.ID, PlannedOutputs: []models.PlannedOutput{}, Warnings: []string{}}

	if jobErr, _ := s.prepareRecipe(args); jobErr != nil {
		payload.Error = jobErr
		return payload, true
	}

	request := models.RecipeRequest{
		RecipeID:   args.ID,
		InputPaths: args.InputPaths,
		OutputDir:  args.OutputDir,
		Options:    args.Options,
	}

	plan := s.recipes.Plan(request)
	if !plan.Success {
		payload.Error = recipePlanError(plan)
		return payload, true
	}
	if jobErr := s.checkPlannedOutputs(plan.Outputs); jobErr != nil {
		payload.Error = jobErr
		return payload, true
	}

	payload.Success = true
	payload.OutputDir = plan.OutputDir
	payload.PlannedOutputs = nonNilPlannedOutputs(plan.Outputs)
	payload.Warnings = nonNilStrings(plan.Warnings)

	return payload, false
}

// outputEntry is one produced path and its size in bytes.
type outputEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

type runJobPayload struct {
	Success bool             `json:"success"`
	ToolID  string           `json:"toolId"`
	JobID   string           `json:"jobId,omitempty"`
	Status  string           `json:"status"`
	Outputs []outputEntry    `json:"outputs"`
	Error   *models.JobError `json:"error,omitempty"`

	// inputCount stays out of the protocol response; the audit line records it.
	inputCount int
}

type runRecipePayload struct {
	Success  bool             `json:"success"`
	RecipeID string           `json:"recipeId"`
	JobID    string           `json:"jobId,omitempty"`
	Status   string           `json:"status"`
	Outputs  []outputEntry    `json:"outputs"`
	Error    *models.JobError `json:"error,omitempty"`

	// inputCount stays out of the protocol response; the audit line records it.
	inputCount int
}

func (s *Server) handleRunJob(ctx context.Context, raw json.RawMessage) (any, bool) {
	payload, auditStatus, auditDir, auditTool, jobErr := s.executeRunJob(ctx, raw)
	s.recordAudit(auditTool, payload.inputCount, auditDir, auditStatus, errorDetailCode(jobErr))

	return payload, jobErr != nil
}

func (s *Server) executeRunJob(ctx context.Context, raw json.RawMessage) (runJobPayload, string, string, string, *models.JobError) {
	auditStatus := "rejected"
	payload := runJobPayload{Success: false, Status: models.JobStatusFailed, Outputs: []outputEntry{}}

	args, jobErr := decodeJobArguments(raw, "toolId")
	if jobErr != nil {
		payload.Error = jobErr
		return payload, auditStatus, "", "", jobErr
	}
	payload.ToolID = args.ID
	payload.inputCount = len(args.InputPaths)
	auditTool := args.ID
	auditDir := strings.TrimSpace(args.OutputDir)

	mode, jobErr, denied := s.prepareJob(args)
	if jobErr != nil {
		if denied {
			auditStatus = "denied"
		}
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}

	request := models.JobRequest{
		ToolID:     args.ID,
		Mode:       mode,
		InputPaths: args.InputPaths,
		OutputDir:  args.OutputDir,
		Options:    args.Options,
	}

	plan := s.tooling.PlanJob(request)
	if !plan.Success {
		jobErr = planError(plan)
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}
	auditDir = effectiveOutputDir(auditDir, plan.Outputs)
	if jobErr = s.checkPlannedOutputs(plan.Outputs); jobErr != nil {
		auditStatus = "denied"
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}

	submitted := s.tooling.RunJob(request)
	if !submitted.Success {
		jobErr = submitted.Error
		if jobErr == nil {
			jobErr = models.NewCanonicalJobError("VALIDATION_ERROR", submitted.Message, nil)
		}
		if jobErr.DetailCode == "SUBMIT_ERROR" {
			auditStatus = "failed"
		}
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}

	payload.JobID = submitted.JobID
	result, waitErr := s.tooling.Engine().Wait(ctx, submitted.JobID)
	if waitErr != nil && !isTerminalJobStatus(result.Status) {
		jobErr = models.NewCanonicalJobError("EXECUTION_FAILED", fmt.Sprintf("waiting for job '%s': %v", submitted.JobID, waitErr), nil)
		payload.Status = models.JobStatusFailed
		payload.Error = jobErr
		return payload, "failed", auditDir, auditTool, jobErr
	}

	if result.Error == nil {
		result.Error = firstItemError(result.Items)
	}

	payload.Status = result.Status
	payload.Success = result.Status == models.JobStatusSuccess
	payload.Error = result.Error
	auditStatus = result.Status

	paths := jobOutputPaths(result, plan)
	if jobErr = s.checkOutputPaths(paths); jobErr != nil {
		payload.Success = false
		payload.Outputs = []outputEntry{}
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}
	payload.Outputs = outputEntries(paths)

	return payload, auditStatus, auditDir, auditTool, result.Error
}

func (s *Server) handleRunRecipe(ctx context.Context, raw json.RawMessage) (any, bool) {
	payload, auditStatus, auditDir, auditTool, jobErr := s.executeRunRecipe(ctx, raw)
	s.recordAudit(auditTool, payload.inputCount, auditDir, auditStatus, errorDetailCode(jobErr))

	return payload, jobErr != nil
}

func (s *Server) executeRunRecipe(ctx context.Context, raw json.RawMessage) (runRecipePayload, string, string, string, *models.JobError) {
	auditStatus := "rejected"
	payload := runRecipePayload{Success: false, Status: models.JobStatusFailed, Outputs: []outputEntry{}}

	args, jobErr := decodeJobArguments(raw, "recipeId")
	if jobErr != nil {
		payload.Error = jobErr
		return payload, auditStatus, "", "", jobErr
	}
	payload.RecipeID = args.ID
	payload.inputCount = len(args.InputPaths)
	auditTool := args.ID
	auditDir := strings.TrimSpace(args.OutputDir)

	if jobErr, denied := s.prepareRecipe(args); jobErr != nil {
		if denied {
			auditStatus = "denied"
		}
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}

	request := models.RecipeRequest{
		RecipeID:   args.ID,
		InputPaths: args.InputPaths,
		OutputDir:  args.OutputDir,
		Options:    args.Options,
	}

	plan := s.recipes.Plan(request)
	if !plan.Success {
		jobErr = recipePlanError(plan)
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}
	auditDir = effectiveRecipeOutputDir(auditDir, plan)
	if jobErr = s.checkPlannedOutputs(plan.Outputs); jobErr != nil {
		auditStatus = "denied"
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}

	submitted := s.recipes.Run(ctx, request)
	if !submitted.Success {
		jobErr = submitted.Error
		if jobErr == nil {
			jobErr = models.NewCanonicalJobError("VALIDATION_ERROR", submitted.Message, nil)
		}
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}

	payload.JobID = submitted.JobID
	result, waitErr := waitRecipe(ctx, s.recipes, submitted.JobID)
	if waitErr != nil && !isTerminalJobStatus(result.Status) {
		jobErr = models.NewCanonicalJobError("EXECUTION_FAILED", fmt.Sprintf("waiting for recipe '%s': %v", submitted.JobID, waitErr), nil)
		payload.Status = models.JobStatusFailed
		payload.Error = jobErr
		return payload, "failed", auditDir, auditTool, jobErr
	}

	payload.Status = result.Status
	payload.Success = result.Status == models.JobStatusSuccess
	payload.Error = result.Error
	auditStatus = result.Status

	if jobErr = s.checkOutputPaths(result.Outputs); jobErr != nil {
		payload.Success = false
		payload.Outputs = []outputEntry{}
		payload.Error = jobErr
		return payload, auditStatus, auditDir, auditTool, jobErr
	}
	payload.Outputs = outputEntries(result.Outputs)

	return payload, auditStatus, auditDir, auditTool, result.Error
}

func (s *Server) recordAudit(tool string, inputCount int, outputDir, status, errorCode string) {
	if s.audit == nil {
		return
	}

	if err := s.audit.Append(AuditEntry{
		Time:       time.Now().UTC(),
		Tool:       tool,
		InputCount: inputCount,
		OutputDir:  outputDir,
		Status:     status,
		ErrorCode:  errorCode,
	}); err != nil {
		s.logf("audit append failed: %v", err)
	}
}

func waitRecipe(ctx context.Context, service recipeService, jobID string) (models.RecipeResult, error) {
	ticker := time.NewTicker(recipePollInterval)
	defer ticker.Stop()

	for {
		status := service.Status(jobID)
		if !status.Success || status.Result == nil {
			message := status.Message
			if status.Error != nil && strings.TrimSpace(status.Error.Message) != "" {
				message = status.Error.Message
			}
			if message == "" {
				message = "recipe run not found"
			}
			return models.RecipeResult{}, errors.New(message)
		}

		result := *status.Result
		if isTerminalJobStatus(result.Status) {
			return result, nil
		}

		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}
	}
}

// recipeService is the narrow slice of the recipe service the server waits on.
type recipeService interface {
	Status(jobID string) models.RecipeStatusResponse
}

func effectiveOutputDir(requested string, outputs []models.PlannedOutput) string {
	if strings.TrimSpace(requested) != "" {
		return requested
	}
	for _, output := range outputs {
		path := strings.TrimSpace(output.OutputPath)
		if path == "" {
			continue
		}
		if output.OutputKind == "directory" {
			return path
		}
		return filepath.Dir(path)
	}

	return ""
}

func effectiveRecipeOutputDir(requested string, plan models.RecipePlanResponse) string {
	if strings.TrimSpace(plan.OutputDir) != "" {
		return plan.OutputDir
	}
	if strings.TrimSpace(requested) != "" {
		return requested
	}

	return effectiveOutputDir("", plan.Outputs)
}

func jobOutputPaths(result models.JobResult, plan models.PlanJobResponse) []string {
	paths := make([]string, 0, len(result.Items))
	seen := make(map[string]struct{}, len(result.Items))
	add := func(path string) {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			return
		}
		key := filepath.Clean(trimmed)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		paths = append(paths, trimmed)
	}

	for _, item := range result.Items {
		if len(item.Outputs) > 0 {
			for _, path := range item.Outputs {
				add(path)
			}
			continue
		}
		add(item.OutputPath)
	}

	if len(paths) == 0 {
		for _, output := range plan.Outputs {
			add(output.OutputPath)
		}
	}

	return paths
}

func firstItemError(items []models.JobResultItem) *models.JobError {
	for _, item := range items {
		if item.Error != nil {
			return item.Error
		}
	}

	return nil
}

func planError(plan models.PlanJobResponse) *models.JobError {
	if plan.Error != nil {
		return plan.Error
	}

	message := strings.TrimSpace(plan.Message)
	if message == "" {
		message = "job request could not be planned"
	}

	return models.NewCanonicalJobError("VALIDATION_ERROR", message, nil)
}

func recipePlanError(plan models.RecipePlanResponse) *models.JobError {
	if plan.Error != nil {
		return plan.Error
	}

	message := strings.TrimSpace(plan.Message)
	if message == "" {
		message = "recipe request could not be planned"
	}

	return models.NewCanonicalJobError("VALIDATION_ERROR", message, nil)
}

func errorDetailCode(jobErr *models.JobError) string {
	if jobErr == nil {
		return ""
	}
	if strings.TrimSpace(jobErr.DetailCode) != "" {
		return jobErr.DetailCode
	}

	return jobErr.Code
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case models.JobStatusSuccess,
		models.JobStatusPartialSuccess,
		models.JobStatusFailed,
		models.JobStatusCancelled,
		models.JobStatusInterrupted:
		return true
	default:
		return false
	}
}

func outputEntries(paths []string) []outputEntry {
	entries := make([]outputEntry, 0, len(paths))
	for _, path := range paths {
		entries = append(entries, outputEntry{Path: path, SizeBytes: pathSize(path)})
	}

	return entries
}

func pathSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}

	var total int64
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil {
			total += info.Size()
		}

		return nil
	})

	return total
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}

	return values
}

func nonNilPlannedOutputs(outputs []models.PlannedOutput) []models.PlannedOutput {
	if outputs == nil {
		return []models.PlannedOutput{}
	}

	return outputs
}
