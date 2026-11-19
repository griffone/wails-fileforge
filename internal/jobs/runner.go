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

	state.updateProgress(models.JobProgressV1{
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

	if state.request.Mode == "single" {
		e.runSingle(state)
		return
	}

	e.runBatch(state)
}

// runSingle prefers progress-aware single execution and falls back to the
// plain single executor.
func (e *Engine) runSingle(state *jobState) {
	var execute func() (models.JobResultItemV1, *models.JobErrorV1)

	if withProgress, ok := state.tool.(tools.SingleExecutorWithProgress); ok {
		execute = func() (models.JobResultItemV1, *models.JobErrorV1) {
			return withProgress.ExecuteSingleWithProgress(state.ctx, state.request, state.updateProgress)
		}
	} else if single, ok := state.tool.(tools.SingleExecutor); ok {
		execute = func() (models.JobResultItemV1, *models.JobErrorV1) {
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
		state.complete(StatusCancelled, "job cancelled", []models.JobResultItemV1{item}, cancelledJobError(state.ctx, attempts, jobErr), time.Now().UnixMilli())
		return
	}
	if jobErr != nil {
		state.complete(StatusFailed, "job failed", []models.JobResultItemV1{item}, normalizeJobError(jobErr), time.Now().UnixMilli())
		return
	}

	state.complete(StatusSuccess, "job success", []models.JobResultItemV1{item}, nil, time.Now().UnixMilli())
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
	items := make([]models.JobResultItemV1, 0, originalTotal)
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
	var lastErr *models.JobErrorV1

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

// batchProgressHandler maps retry-local progress back to original input
// positions so the UI never jumps backwards.
func (e *Engine) batchProgressHandler(state *jobState, retrying bool, originalIndices []int, total int) func(models.JobProgressV1) {
	return func(value models.JobProgressV1) {
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
func (e *Engine) retryableFailedIndices(toolID string, items []models.JobResultItemV1, total int, execErr *models.JobErrorV1) []int {
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

func (e *Engine) retryable(toolID string, jobErr *models.JobErrorV1) bool {
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
func mergeRetryBatchItems(items []models.JobResultItemV1, retryItems []models.JobResultItemV1, retryIndices []int, retryPaths []string, inputPaths []string) []models.JobResultItemV1 {
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

	apply := func(targets []int, item models.JobResultItemV1) {
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

func (e *Engine) executeSingleWithRetry(state *jobState, execute func() (models.JobResultItemV1, *models.JobErrorV1)) (models.JobResultItemV1, *models.JobErrorV1, int) {
	attempts := 0
	maxAttempts := state.maxAttempts
	var item models.JobResultItemV1
	var jobErr *models.JobErrorV1

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

func decorateSingleError(item *models.JobResultItemV1, jobErr *models.JobErrorV1, attempts int) {
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
func cancelledJobError(ctx context.Context, attempts int, lastErr *models.JobErrorV1) *models.JobErrorV1 {
	message := "job cancelled"
	if ctx != nil && ctx.Err() != nil {
		message = ctx.Err().Error()
	}

	return models.NewCanonicalJobError("JOB_CANCELLED", message, retryMetadata(attempts, lastErr))
}
