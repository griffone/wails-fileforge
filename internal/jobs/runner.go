package jobs

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/logx"
	"fileforge-desktop/internal/tools"
)

// runJob executes one scheduled job. It owns the release of its capacity slot
// and converts a tool panic into a failed job so the engine survives.
func (e *Engine) runJob(state *jobState) {
	defer e.wg.Done()
	defer e.release(state.class)
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}

		stack := debug.Stack()
		logger := logx.WithJob(e.logger, state.result.JobID, state.result.ToolID)
		logger.Error("job panicked", "panic", fmt.Sprintf("%v", recovered), "stack", string(stack))

		state.complete(
			StatusFailed,
			"job failed",
			nil,
			models.NewCanonicalJobError(enginePanicDetail, fmt.Sprintf("job panicked: %v", recovered), map[string]any{
				"panic": fmt.Sprintf("%v", recovered),
			}),
			time.Now().UnixMilli(),
		)
	}()

	state.updateProgress(models.JobProgress{
		Current: 0,
		Total:   len(state.request.InputPaths),
		Stage:   StatusRunning,
		Message: "running",
	})

	select {
	case <-state.ctx.Done():
		state.complete(StatusCancelled, "job cancelled", nil, cancelledJobError(state.ctx, 0, nil), time.Now().UnixMilli())
		return
	default:
	}

	if state.plan != nil {
		if executor, ok := state.tool.(tools.PlanExecutor); ok {
			e.runPlanned(state, executor)
			return
		}
	}

	if state.request.Mode == "single" {
		e.runSingle(state)
		return
	}

	e.runBatch(state)
}

// runSingle prefers progress-aware single execution and falls back to the
// plain single executor.
func (e *Engine) runSingle(state *jobState) {
	var execute func() (models.JobResultItem, *models.JobError)

	if withProgress, ok := state.tool.(tools.SingleExecutorWithProgress); ok {
		execute = func() (models.JobResultItem, *models.JobError) {
			return withProgress.ExecuteSingleWithProgress(state.ctx, state.request, state.updateProgress)
		}
	} else if single, ok := state.tool.(tools.SingleExecutor); ok {
		execute = func() (models.JobResultItem, *models.JobError) {
			return single.ExecuteSingle(state.ctx, state.request)
		}
	} else {
		state.complete(StatusFailed, "job failed", nil,
			models.NewCanonicalJobError("UNSUPPORTED_MODE", "single execution not supported", nil),
			time.Now().UnixMilli())
		return
	}

	item, jobErr, attempts := e.executeSingleWithRetry(state, execute)
	item = normalizeItemError(item)
	item.Attempts = attempts
	item.RetryCount = max(0, attempts-1)

	if state.ctx.Err() != nil {
		state.complete(StatusCancelled, "job cancelled", []models.JobResultItem{item}, cancelledJobError(state.ctx, attempts, jobErr), time.Now().UnixMilli())
		return
	}
	if jobErr != nil {
		state.complete(StatusFailed, "job failed", []models.JobResultItem{item}, normalizeJobError(jobErr), time.Now().UnixMilli())
		return
	}

	state.complete(StatusSuccess, "job success", []models.JobResultItem{item}, nil, time.Now().UnixMilli())
}

// runBatch retries only failed, retryable items. Retry inputs are deduplicated
// by path and their results fan back out to every original index that shares
// the path, preserving input order in the final result.
func (e *Engine) runBatch(state *jobState) {
	exec, ok := state.tool.(tools.BatchExecutor)
	if !ok {
		state.complete(StatusFailed, "job failed", nil,
			models.NewCanonicalJobError("UNSUPPORTED_MODE", "batch execution not supported", nil),
			time.Now().UnixMilli())
		return
	}

	req := state.request
	originalTotal := len(req.InputPaths)
	items := make([]models.JobResultItem, 0, originalTotal)
	attempts := make([]int, originalTotal)
	for index := range attempts {
		attempts[index] = 1
	}

	currentIndices := make([]int, originalTotal)
	for index := range currentIndices {
		currentIndices[index] = index
	}

	currentReq := req
	retries := 0
	var lastErr *models.JobError

	for {
		if state.ctx.Err() != nil {
			items = normalizeItemsErrors(items)
			items = decorateBatchItems(items, attempts)
			state.complete(StatusCancelled, "job cancelled", items, cancelledJobError(state.ctx, maxAttemptCount(attempts), lastErr), time.Now().UnixMilli())
			return
		}

		retrying := retries > 0
		batchItems, execErr := exec.ExecuteBatch(state.ctx, currentReq, e.batchProgressHandler(state, retrying, currentIndices, originalTotal))
		lastErr = execErr

		if len(batchItems) == 0 {
			break
		}

		if retrying {
			items = mergeRetryBatchItems(items, batchItems, currentIndices, currentReq.InputPaths, req.InputPaths)
			for _, index := range currentIndices {
				attempts[index]++
			}
		} else {
			items = alignBatchItems(batchItems, req.InputPaths)
		}

		retryIndices := e.retryableFailedIndices(req.ToolID, items, originalTotal, execErr)
		if len(retryIndices) == 0 || retries+1 >= state.maxAttempts {
			break
		}

		if !waitRetryBackoff(state.ctx, retries+1) {
			items = normalizeItemsErrors(items)
			items = decorateBatchItems(items, attempts)
			state.complete(StatusCancelled, "job cancelled", items, cancelledJobError(state.ctx, maxAttemptCount(attempts), execErr), time.Now().UnixMilli())
			return
		}

		retries++
		currentIndices = retryIndices
		currentReq = req
		currentReq.InputPaths = dedupeRetryPaths(req.InputPaths, retryIndices)
	}

	items = normalizeItemsErrors(items)
	items = decorateBatchItems(items, attempts)
	aggregateAttempts := maxAttemptCount(attempts)

	if state.ctx.Err() != nil {
		state.complete(StatusCancelled, "job cancelled", items, cancelledJobError(state.ctx, aggregateAttempts, lastErr), time.Now().UnixMilli())
		return
	}

	if lastErr != nil {
		status, message := deriveBatchFinalState(items)
		jobErr := normalizeJobError(lastErr)
		if jobErr != nil {
			if jobErr.Details == nil {
				jobErr.Details = map[string]any{}
			}
			for key, value := range retryMetadata(aggregateAttempts, lastErr) {
				jobErr.Details[key] = value
			}
		}
		state.complete(status, message, items, jobErr, time.Now().UnixMilli())
		return
	}

	status, message := deriveBatchFinalState(items)
	state.complete(status, message, items, nil, time.Now().UnixMilli())
}

// runPlanned executes the stored plan and never re-plans. Retries reuse the
// original outputs, resend only the failed ones, and keep the final result in
// original plan order with per-item attempts decorated.
func (e *Engine) runPlanned(state *jobState, executor tools.PlanExecutor) {
	plan := cloneJobPlan(state.plan)
	if plan == nil {
		state.complete(StatusFailed, "job failed", nil,
			models.NewCanonicalJobError("PLAN_MISSING", "planned job has no stored plan", nil),
			time.Now().UnixMilli())
		return
	}

	planOutputs := plan.Outputs
	originalTotal := len(planOutputs)
	items := make([]models.JobResultItem, 0, originalTotal)
	attempts := make([]int, originalTotal)
	for index := range attempts {
		attempts[index] = 1
	}

	currentIndices := make([]int, originalTotal)
	for index := range currentIndices {
		currentIndices[index] = index
	}

	currentPlan := *plan
	currentPlan.Outputs = selectPlanOutputs(planOutputs, currentIndices)
	retries := 0
	var lastErr *models.JobError

	for {
		if state.ctx.Err() != nil {
			items = decorateBatchItems(normalizeItemsErrors(items), attempts)
			state.complete(StatusCancelled, "job cancelled", items, cancelledJobError(state.ctx, maxAttemptCount(attempts), lastErr), time.Now().UnixMilli())
			return
		}

		retrying := retries > 0
		plannedItems, execErr := executor.ExecutePlan(state.ctx, state.request, currentPlan, e.batchProgressHandler(state, retrying, currentIndices, originalTotal))
		lastErr = execErr

		if retrying {
			items = mergePlannedRetryItems(items, plannedItems, currentIndices, currentPlan.Outputs)
			for _, index := range currentIndices {
				attempts[index]++
			}
		} else {
			items = alignPlannedItems(plannedItems, planOutputs)
		}

		retryIndices := e.retryableFailedIndices(state.request.ToolID, items, originalTotal, execErr)
		if len(retryIndices) == 0 || retries+1 >= state.maxAttempts {
			break
		}

		if !waitRetryBackoff(state.ctx, retries+1) {
			items = decorateBatchItems(normalizeItemsErrors(items), attempts)
			state.complete(StatusCancelled, "job cancelled", items, cancelledJobError(state.ctx, maxAttemptCount(attempts), execErr), time.Now().UnixMilli())
			return
		}

		retries++
		currentIndices = retryIndices
		currentPlan.Outputs = selectPlanOutputs(planOutputs, retryIndices)
	}

	items = decorateBatchItems(normalizeItemsErrors(items), attempts)
	aggregateAttempts := maxAttemptCount(attempts)

	if state.ctx.Err() != nil {
		state.complete(StatusCancelled, "job cancelled", items, cancelledJobError(state.ctx, aggregateAttempts, lastErr), time.Now().UnixMilli())
		return
	}

	if lastErr != nil {
		status, message := deriveBatchFinalState(items)
		jobErr := normalizeJobError(lastErr)
		if jobErr != nil {
			if jobErr.Details == nil {
				jobErr.Details = map[string]any{}
			}
			for key, value := range retryMetadata(aggregateAttempts, lastErr) {
				jobErr.Details[key] = value
			}
		}
		state.complete(status, message, items, jobErr, time.Now().UnixMilli())
		return
	}

	status, message := deriveBatchFinalState(items)
	state.complete(status, message, items, nil, time.Now().UnixMilli())
}

// selectPlanOutputs returns the outputs at indices in order, cloned so a
// reduced retry plan never aliases the stored plan.
func selectPlanOutputs(outputs []models.PlannedOutput, indices []int) []models.PlannedOutput {
	selected := make([]models.PlannedOutput, 0, len(indices))
	for _, index := range indices {
		if index < 0 || index >= len(outputs) {
			continue
		}
		selected = append(selected, clonePlannedOutput(outputs[index]))
	}

	return selected
}

// alignPlannedItems maps returned items onto planned outputs by input path
// while preserving plan order. Missing outputs become PLAN_EXECUTION_MISMATCH
// failures; unplanned items are appended unchanged in the tool's order.
func alignPlannedItems(items []models.JobResultItem, outputs []models.PlannedOutput) []models.JobResultItem {
	aligned := make([]models.JobResultItem, len(outputs))
	used := make([]bool, len(items))
	remaining := make(map[string][]int, len(items))
	for index, item := range items {
		if item.InputPath == "" {
			continue
		}
		remaining[item.InputPath] = append(remaining[item.InputPath], index)
	}

	for outIndex, output := range outputs {
		if outIndex < len(items) && !used[outIndex] && items[outIndex].InputPath == output.InputPath {
			used[outIndex] = true
			aligned[outIndex] = items[outIndex]
			continue
		}

		candidates := remaining[output.InputPath]
		for len(candidates) > 0 && used[candidates[0]] {
			candidates = candidates[1:]
		}
		if len(candidates) > 0 {
			used[candidates[0]] = true
			aligned[outIndex] = items[candidates[0]]
			remaining[output.InputPath] = candidates[1:]
			continue
		}

		aligned[outIndex] = plannedMismatchItem(output)
	}

	for index, item := range items {
		if used[index] {
			continue
		}
		aligned = append(aligned, item)
	}

	return aligned
}

func plannedMismatchItem(output models.PlannedOutput) models.JobResultItem {
	message := "tool returned no result for a planned output"
	item := models.JobResultItem{
		InputPath: output.InputPath,
		Success:   false,
		Message:   message,
		Error: models.NewCanonicalJobError("PLAN_EXECUTION_MISMATCH", message, map[string]any{
			"plannedOutputPath": output.OutputPath,
		}),
	}
	if output.OutputPath != "" {
		item.OutputPath = output.OutputPath
		item.Outputs = []string{output.OutputPath}
		item.OutputCount = 1
	}

	return item
}

// mergePlannedRetryItems copies retry results back onto the original output
// positions after aligning them to the reduced retry plan.
func mergePlannedRetryItems(items, retryItems []models.JobResultItem, retryIndices []int, retryOutputs []models.PlannedOutput) []models.JobResultItem {
	aligned := alignPlannedItems(retryItems, retryOutputs)
	for position, index := range retryIndices {
		if index < 0 || index >= len(items) || position >= len(retryOutputs) || position >= len(aligned) {
			continue
		}
		items[index] = cloneJobItem(aligned[position])
	}

	return items
}

// batchProgressHandler maps retry-local progress back to original input
// positions so the UI never jumps backwards.
func (e *Engine) batchProgressHandler(state *jobState, retrying bool, originalIndices []int, total int) func(models.JobProgress) {
	return func(value models.JobProgress) {
		if retrying {
			current := value.Current
			if current >= 1 && current <= len(originalIndices) {
				current = originalIndices[current-1] + 1
			}
			value.Current = current
			value.Total = total
			value.Stage = "retrying"
		}

		state.updateProgress(value)
	}
}

// retryableFailedIndices returns failed item indices whose error is retryable.
// When an item carries no error, the batch-level error is used as fallback.
func (e *Engine) retryableFailedIndices(toolID string, items []models.JobResultItem, total int, execErr *models.JobError) []int {
	failed := make([]int, 0)
	for index := 0; index < total && index < len(items); index++ {
		if items[index].Success {
			continue
		}

		itemErr := items[index].Error
		if itemErr == nil {
			itemErr = execErr
		}
		if e.retryable(toolID, itemErr) {
			failed = append(failed, index)
		}
	}

	return failed
}

func (e *Engine) retryable(toolID string, jobErr *models.JobError) bool {
	if jobErr == nil {
		return false
	}
	if e.retry != nil {
		return e.retry.Retryable(toolID, jobErr)
	}

	return isRetryableError(toolID, jobErr)
}

// dedupeRetryPaths builds the retry request inputs, keeping the first
// occurrence of every path while preserving failed-index order.
func dedupeRetryPaths(inputPaths []string, indices []int) []string {
	seen := make(map[string]struct{}, len(indices))
	paths := make([]string, 0, len(indices))
	for _, index := range indices {
		if index < 0 || index >= len(inputPaths) {
			continue
		}
		path := inputPaths[index]
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}

	return paths
}

// mergeRetryBatchItems copies retry results back onto the original item
// positions. Paths are matched first; a tool that echoes a different path falls
// back to the retry request position.
func mergeRetryBatchItems(items []models.JobResultItem, retryItems []models.JobResultItem, retryIndices []int, retryPaths []string, inputPaths []string) []models.JobResultItem {
	targetsByPath := make(map[string][]int, len(retryPaths))
	allowed := make(map[int]struct{}, len(retryIndices))
	for _, index := range retryIndices {
		if index < 0 || index >= len(inputPaths) {
			continue
		}
		allowed[index] = struct{}{}
		path := inputPaths[index]
		targetsByPath[path] = append(targetsByPath[path], index)
	}

	apply := func(targets []int, item models.JobResultItem) {
		for _, index := range targets {
			if _, ok := allowed[index]; !ok {
				continue
			}
			items[index] = cloneJobItem(item)
		}
	}

	for position, retryItem := range retryItems {
		if targets, ok := targetsByPath[retryItem.InputPath]; ok && retryItem.InputPath != "" {
			apply(targets, retryItem)
			continue
		}
		if position < len(retryPaths) {
			apply(targetsByPath[retryPaths[position]], retryItem)
		}
	}

	return items
}

func (e *Engine) executeSingleWithRetry(state *jobState, execute func() (models.JobResultItem, *models.JobError)) (models.JobResultItem, *models.JobError, int) {
	attempts := 0
	maxAttempts := state.maxAttempts
	var item models.JobResultItem
	var jobErr *models.JobError

	for attempts < maxAttempts {
		attempts++
		item, jobErr = execute()

		if jobErr == nil {
			return item, nil, attempts
		}

		if !e.retryable(state.request.ToolID, jobErr) || attempts >= maxAttempts {
			decorateSingleError(&item, jobErr, attempts)
			return item, jobErr, attempts
		}

		if !waitRetryBackoff(state.ctx, attempts) {
			cancelErr := models.NewCanonicalJobError("JOB_CANCELLED", state.ctx.Err().Error(), retryMetadata(attempts, jobErr))
			if item.Error == nil {
				item.Error = cancelErr
			}
			return item, cancelErr, attempts
		}
	}

	if jobErr == nil {
		jobErr = models.NewCanonicalJobError(defaultEngineDetail, "execution failed without explicit error", nil)
	}
	if item.Error == nil {
		item.Error = jobErr
	}

	return item, jobErr, attempts
}

func decorateSingleError(item *models.JobResultItem, jobErr *models.JobError, attempts int) {
	if item.Error == nil {
		item.Error = jobErr
	}
	if item.Error == nil {
		return
	}
	if item.Error.Details == nil {
		item.Error.Details = map[string]any{}
	}
	for key, value := range retryMetadata(attempts, jobErr) {
		item.Error.Details[key] = value
	}
}

// cancelledJobError builds the canonical cancellation error used by queued and
// running cancellation paths.
func cancelledJobError(ctx context.Context, attempts int, lastErr *models.JobError) *models.JobError {
	message := "job cancelled"
	if ctx != nil && ctx.Err() != nil {
		message = ctx.Err().Error()
	}

	return models.NewCanonicalJobError("JOB_CANCELLED", message, retryMetadata(attempts, lastErr))
}

func normalizeItemError(item models.JobResultItem) models.JobResultItem {
	if item.Error != nil {
		item.Error = normalizeJobError(item.Error)
	}

	return item
}

func normalizeItemsErrors(items []models.JobResultItem) []models.JobResultItem {
	normalized := make([]models.JobResultItem, len(items))
	for index := range items {
		normalized[index] = normalizeItemError(items[index])
	}

	return normalized
}

// deriveBatchFinalState maps per-item outcomes to the job status and message.
func deriveBatchFinalState(items []models.JobResultItem) (string, string) {
	if len(items) == 0 {
		return StatusFailed, "job failed"
	}

	successCount := 0
	for _, item := range items {
		if item.Success {
			successCount++
		}
	}

	switch {
	case successCount == len(items):
		return StatusSuccess, "job success"
	case successCount == 0:
		return StatusFailed, "job failed"
	default:
		return StatusPartialSuccess, "job partial success"
	}
}

// alignBatchItems guarantees one item per input position. Missing results are
// reported as failures and extra results are appended unchanged.
func alignBatchItems(items []models.JobResultItem, inputPaths []string) []models.JobResultItem {
	aligned := make([]models.JobResultItem, 0, max(len(items), len(inputPaths)))
	for index, inputPath := range inputPaths {
		if index < len(items) {
			aligned = append(aligned, items[index])
			continue
		}
		aligned = append(aligned, models.JobResultItem{
			InputPath: inputPath,
			Success:   false,
			Message:   "no result returned for input",
		})
	}

	if len(items) > len(inputPaths) {
		aligned = append(aligned, items[len(inputPaths):]...)
	}

	return aligned
}

func maxAttemptCount(attempts []int) int {
	highest := 1
	for _, attempt := range attempts {
		highest = max(highest, attempt)
	}

	return highest
}

// decorateBatchItems records per-item attempts and derived retry counts.
func decorateBatchItems(items []models.JobResultItem, attempts []int) []models.JobResultItem {
	decorated := make([]models.JobResultItem, len(items))
	for index := range items {
		item := items[index]
		attempt := 1
		if index < len(attempts) && attempts[index] > 0 {
			attempt = attempts[index]
		}
		item.Attempts = attempt
		item.RetryCount = max(0, attempt-1)
		decorated[index] = item
	}

	return decorated
}
