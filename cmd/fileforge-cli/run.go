package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"fileforge-desktop/internal/models"
)

const recipePollInterval = 20 * time.Millisecond

// runFlags is the parsed form of the flags shared by both run commands.
type runFlags struct {
	inputs  []string
	outDir  string
	options map[string]any
	asJSON  bool
}

// parseRunFlags parses one run command. An empty input list is a usage error,
// and flag.ErrHelp is returned unchanged so the caller can exit zero. The flag
// set writes nothing itself: the caller prints every error once.
func parseRunFlags(name string, args []string) (runFlags, error) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var rawInputs stringList
	var rawOptions stringList
	flags.Var(&rawInputs, "input", "input path; repeat for every input")
	flags.Var(&rawOptions, "option", "tool or recipe option as key=value; repeat for every option")
	outDir := flags.String("out", "", "output directory")
	asJSON := flags.Bool("json", false, "print one machine-readable JSON object on stdout")

	if err := flags.Parse(args); err != nil {
		return runFlags{}, err
	}
	if flags.NArg() > 0 {
		return runFlags{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if len(rawInputs) == 0 {
		return runFlags{}, errors.New("at least one --input is required")
	}

	options, err := parseOptions(rawOptions)
	if err != nil {
		return runFlags{}, err
	}

	return runFlags{inputs: rawInputs, outDir: strings.TrimSpace(*outDir), options: options, asJSON: *asJSON}, nil
}

// runTool plans and runs one tool on the shared job engine, then waits for the
// terminal result without any Wails event bus.
func runTool(ctx context.Context, s *stack, toolID string, flags runFlags, stdout, stderr io.Writer) int {
	tool, err := s.registry.GetTool(toolID)
	if err != nil {
		return writeFailure(stdout, stderr, flags.asJSON, exitValidation,
			models.NewCanonicalJobError("TOOL_NOT_FOUND", err.Error(), map[string]any{"toolId": toolID}))
	}

	if unknown, keys, found := unknownToolOption(toolID, flags.options); found && unknown != "" {
		message := fmt.Sprintf("Option '%s' is not supported by tool '%s'. Supported options: %s.", unknown, toolID, strings.Join(keys, ", "))
		return writeFailure(stdout, stderr, flags.asJSON, exitValidation,
			models.NewCanonicalJobError("TOOL_OPTION_UNKNOWN", message, map[string]any{"option": unknown, "toolId": toolID}))
	}

	mode := "single"
	if tool.Manifest().SupportsBatch && len(flags.inputs) > 1 {
		mode = "batch"
	}

	request := models.JobRequest{
		ToolID:     toolID,
		Mode:       mode,
		InputPaths: flags.inputs,
		OutputDir:  flags.outDir,
		Options:    flags.options,
	}

	plan := s.tooling.PlanJob(request)
	if !plan.Success {
		return writeFailure(stdout, stderr, flags.asJSON, exitValidation, planError(plan))
	}

	submitted := s.tooling.RunJob(request)
	if !submitted.Success {
		submitErr := submitted.Error
		if submitErr == nil {
			submitErr = models.NewCanonicalJobError("VALIDATION_ERROR", submitted.Message, nil)
		}
		if submitErr.DetailCode == "SUBMIT_ERROR" {
			return writeFailure(stdout, stderr, flags.asJSON, exitInternal, submitErr)
		}
		return writeFailure(stdout, stderr, flags.asJSON, exitValidation, submitErr)
	}

	result, waitErr := s.tooling.Engine().Wait(ctx, submitted.JobID)
	if waitErr != nil {
		return writeFailure(stdout, stderr, flags.asJSON, exitInternal,
			models.NewCanonicalJobError("EXECUTION_FAILED", fmt.Sprintf("waiting for job '%s': %v", submitted.JobID, waitErr), nil))
	}

	if result.Error == nil {
		result.Error = firstItemError(result.Items)
	}

	report := runReport{
		JobID:   result.JobID,
		Status:  result.Status,
		Success: result.Success,
		Outputs: outputEntries(toolOutputPaths(result, plan)),
		Error:   result.Error,
	}

	return emitRun(stdout, stderr, flags.asJSON, report)
}

// runRecipe plans and runs one recipe, then polls Status for the terminal
// snapshot. Polling keeps the CLI independent from the app's event bus.
func runRecipe(ctx context.Context, s *stack, recipeID string, flags runFlags, stdout, stderr io.Writer) int {
	request := models.RecipeRequest{
		RecipeID:   recipeID,
		InputPaths: flags.inputs,
		OutputDir:  flags.outDir,
		Options:    flags.options,
	}

	plan := s.recipes.Plan(request)
	if !plan.Success {
		return writeFailure(stdout, stderr, flags.asJSON, exitValidation, recipePlanError(plan))
	}

	submitted := s.recipes.Run(ctx, request)
	if !submitted.Success {
		submitErr := submitted.Error
		if submitErr == nil {
			submitErr = models.NewCanonicalJobError("VALIDATION_ERROR", submitted.Message, nil)
		}
		return writeFailure(stdout, stderr, flags.asJSON, exitValidation, submitErr)
	}

	result, waitErr := waitRecipe(ctx, s, submitted.JobID)
	if waitErr != nil && !isTerminalStatus(result.Status) {
		return writeFailure(stdout, stderr, flags.asJSON, exitInternal,
			models.NewCanonicalJobError("EXECUTION_FAILED", fmt.Sprintf("waiting for recipe '%s': %v", submitted.JobID, waitErr), nil))
	}

	report := runReport{
		JobID:   result.JobID,
		Status:  result.Status,
		Success: result.Status == models.JobStatusSuccess,
		Outputs: outputEntries(result.Outputs),
		Error:   result.Error,
	}

	return emitRun(stdout, stderr, flags.asJSON, report)
}

// waitRecipe returns as soon as the run is terminal, or with the last snapshot
// when ctx ends first.
func waitRecipe(ctx context.Context, s *stack, jobID string) (models.RecipeResult, error) {
	ticker := time.NewTicker(recipePollInterval)
	defer ticker.Stop()

	for {
		status := s.recipes.Status(jobID)
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
		if isTerminalStatus(result.Status) {
			return result, nil
		}

		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}
	}
}

// toolOutputPaths collects the produced paths, falling back to the planned
// outputs when a tool reports none. Duplicate paths appear once, in order.
func toolOutputPaths(result models.JobResult, plan models.PlanJobResponse) []string {
	paths := make([]string, 0, len(result.Items))
	seen := make(map[string]struct{}, len(result.Items))
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
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

// firstItemError returns the first canonical error from the result items.
func firstItemError(items []models.JobResultItem) *models.JobError {
	for _, item := range items {
		if item.Error != nil {
			return item.Error
		}
	}

	return nil
}

// planError falls back to a canonical validation error when a failed plan omits
// its typed error.
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

// recipePlanError is planError for recipe plans.
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
