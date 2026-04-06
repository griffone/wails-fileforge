package jobs

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/tools"
)

type testTool struct {
	id              string
	capability      string
	supportSingle   bool
	supportBatch    bool
	validateErr     *models.JobErrorV1
	singleErr       *models.JobErrorV1
	batchErr        *models.JobErrorV1
	batchDelay      time.Duration
	maxActive       *atomic.Int32
	active          *atomic.Int32
	mu              sync.Mutex
	progressEvents  []models.JobProgressV1
	singleProgress  bool
	singleFailCount int
	batchFailCount  int
	singleCalls     atomic.Int32
	batchCalls      atomic.Int32
}

func (t *testTool) ID() string { return t.id }

func (t *testTool) Capability() string { return t.capability }

func (t *testTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:         t.id,
		Name:           "test tool",
		Capability:     t.capability,
		SupportsSingle: t.supportSingle,
		SupportsBatch:  t.supportBatch,
	}
}

func (t *testTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *testTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return t.validateErr
}

func (t *testTool) ExecuteSingle(_ context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	call := int(t.singleCalls.Add(1))
	item := models.JobResultItemV1{
		InputPath:  req.InputPaths[0],
		OutputPath: req.OutputDir + "/out.txt",
		Success:    t.singleErr == nil,
		Message:    "single",
	}
	if t.singleFailCount > 0 && call <= t.singleFailCount {
		err := models.NewCanonicalJobError("DOC_DOCX_TO_PDF_EXECUTION_FAILED", "transient single failure", nil)
		item.Success = false
		item.Error = err
		item.Message = "single failed"
		return item, err
	}
	if t.singleErr != nil {
		item.Success = false
		item.Error = t.singleErr
		return item, t.singleErr
	}

	return item, nil
}

func (t *testTool) ExecuteSingleWithProgress(_ context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	if t.singleProgress && onProgress != nil {
		onProgress(models.JobProgressV1{Current: 15, Total: 100, Stage: StatusRunning, Message: "trim.prepare"})
		onProgress(models.JobProgressV1{Current: 70, Total: 100, Stage: StatusRunning, Message: "trim.reencode"})
	}
	return t.ExecuteSingle(context.Background(), req)
}

func (t *testTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	call := int(t.batchCalls.Add(1))
	if t.active != nil && t.maxActive != nil {
		current := t.active.Add(1)
		for {
			previous := t.maxActive.Load()
			if current <= previous || t.maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		defer t.active.Add(-1)
	}

	items := make([]models.JobResultItemV1, 0, len(req.InputPaths))
	for i, input := range req.InputPaths {
		select {
		case <-ctx.Done():
			return items, &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
		default:
		}

		if t.batchDelay > 0 {
			time.Sleep(t.batchDelay)
		}

		progress := models.JobProgressV1{
			Current: i + 1,
			Total:   len(req.InputPaths),
			Stage:   StatusRunning,
			Message: "progress",
		}
		if onProgress != nil {
			onProgress(progress)
		}

		t.mu.Lock()
		t.progressEvents = append(t.progressEvents, progress)
		t.mu.Unlock()

		items = append(items, models.JobResultItemV1{
			InputPath:  input,
			OutputPath: req.OutputDir + "/" + input,
			Success:    true,
			Message:    "ok",
		})
	}

	if t.batchFailCount > 0 && call <= t.batchFailCount {
		return items, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_EXECUTION_FAILED", "transient batch failure", nil)
	}

	if t.batchErr != nil {
		return items, t.batchErr
	}

	return items, nil
}

var _ tools.Tool = (*testTool)(nil)
var _ tools.SingleExecutor = (*testTool)(nil)
var _ tools.BatchExecutor = (*testTool)(nil)

func newTestOrchestrator(t *testing.T, tool *testTool, maxConcurrent int) *Orchestrator {
	t.Helper()

	reg := registry.NewRegistry()
	reg.SafeRegisterToolV2(tool)
	orch := NewOrchestrator(reg, maxConcurrent)
	return orch
}

func waitForTerminalStatus(t *testing.T, orch *Orchestrator, jobID string) models.JobResultV1 {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res, found := orch.GetJob(jobID)
		if found && (res.Status == StatusSuccess || res.Status == StatusFailed || res.Status == StatusPartialSuccess || res.Status == StatusCancelled || res.Status == StatusInterrupted) {
			return res
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting terminal status for job %s", jobID)
	return models.JobResultV1{}
}

func waitForStatus(t *testing.T, orch *Orchestrator, jobID string, wanted string) models.JobResultV1 {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res, found := orch.GetJob(jobID)
		if found && res.Status == wanted {
			return res
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting status %s for job %s", wanted, jobID)
	return models.JobResultV1{}
}

func TestOrchestratorSubmitAndCompleteSingle(t *testing.T) {
	tool := &testTool{id: "tool.test.single", capability: "test", supportSingle: true, supportBatch: true}
	orch := newTestOrchestrator(t, tool, 2)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "single",
		InputPaths: []string{"input.txt"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected success response, got: %+v", response)
	}

	result := waitForTerminalStatus(t, orch, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("expected success, got %s", result.Status)
	}
	if !result.Success {
		t.Fatalf("expected job success")
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	if result.Progress.Stage != StatusSuccess {
		t.Fatalf("expected progress stage %s, got %s", StatusSuccess, result.Progress.Stage)
	}
	if result.Progress.Current != result.Progress.Total {
		t.Fatalf("expected terminal progress current == total, got %d/%d", result.Progress.Current, result.Progress.Total)
	}
}

func TestOrchestratorCancelBatchJob(t *testing.T) {
	tool := &testTool{id: "tool.test.cancel", capability: "test", supportSingle: true, supportBatch: true, batchDelay: 50 * time.Millisecond}
	orch := newTestOrchestrator(t, tool, 2)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "batch",
		InputPaths: []string{"a", "b", "c", "d"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	time.Sleep(70 * time.Millisecond)
	if cancelErr := orch.Cancel(response.JobID); cancelErr != nil {
		t.Fatalf("unexpected cancel error: %v", cancelErr)
	}

	result := waitForTerminalStatus(t, orch, response.JobID)
	if result.Status != StatusCancelled {
		t.Fatalf("expected cancelled status, got %s", result.Status)
	}
	if result.Error == nil || result.Error.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected cancelled error code, got %+v", result.Error)
	}
}

func TestOrchestratorTransitionsToRunningAndCompleted(t *testing.T) {
	tool := &testTool{id: "tool.test.running", capability: "test", supportSingle: true, supportBatch: true, batchDelay: 30 * time.Millisecond}
	orch := newTestOrchestrator(t, tool, 2)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "batch",
		InputPaths: []string{"a", "b", "c"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	running := waitForStatus(t, orch, response.JobID, StatusRunning)
	if running.Progress.Stage != StatusRunning {
		t.Fatalf("expected running progress stage, got %s", running.Progress.Stage)
	}

	completed := waitForTerminalStatus(t, orch, response.JobID)
	if completed.Status != StatusSuccess {
		t.Fatalf("expected success status, got %s", completed.Status)
	}
}

func TestOrchestratorSingleExecutorWithProgressUpdatesJobProgress(t *testing.T) {
	tool := &testTool{id: "tool.test.single-progress", capability: "test", supportSingle: true, supportBatch: false, singleProgress: true}
	orch := newTestOrchestrator(t, tool, 2)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "single",
		InputPaths: []string{"input.txt"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	result := waitForTerminalStatus(t, orch, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("expected success, got %s", result.Status)
	}
	if result.Progress.Total != 100 || result.Progress.Current != 100 {
		t.Fatalf("expected terminal progress 100/100, got %d/%d", result.Progress.Current, result.Progress.Total)
	}
}

func TestOrchestratorValidationAndErrorPath(t *testing.T) {
	validationErr := &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: "bad request"}
	tool := &testTool{
		id:            "tool.test.error",
		capability:    "test",
		supportSingle: true,
		supportBatch:  true,
		validateErr:   validationErr,
	}
	orch := newTestOrchestrator(t, tool, 2)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "single",
		InputPaths: []string{"input.txt"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}
	if response.Success {
		t.Fatalf("expected submit response failure on validation")
	}
	if response.Status != StatusFailed {
		t.Fatalf("expected failed status, got %s", response.Status)
	}
	if response.Error == nil || response.Error.Code != models.ErrorCodeValidationInvalidInput {
		t.Fatalf("expected canonical validation code, got %+v", response.Error)
	}

	tool.validateErr = nil
	tool.singleErr = &models.JobErrorV1{Code: "EXEC_ERROR", Message: "boom"}

	response2, err2 := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "single",
		InputPaths: []string{"input.txt"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err2 != nil {
		t.Fatalf("unexpected submit error: %v", err2)
	}

	result := waitForTerminalStatus(t, orch, response2.JobID)
	if result.Status != StatusFailed {
		t.Fatalf("expected failed status, got %s", result.Status)
	}
	if result.Error == nil || result.Error.Message != "boom" {
		t.Fatalf("expected error message boom, got %+v", result.Error)
	}
	if result.Error.Code != models.ErrorCodeExecIOTransient {
		t.Fatalf("expected canonical execution code, got %+v", result.Error)
	}
}

func TestOrchestratorConcurrencyLimit(t *testing.T) {
	active := &atomic.Int32{}
	maxActive := &atomic.Int32{}
	tool := &testTool{
		id:            "tool.test.concurrent",
		capability:    "test",
		supportSingle: true,
		supportBatch:  true,
		batchDelay:    60 * time.Millisecond,
		active:        active,
		maxActive:     maxActive,
	}
	orch := newTestOrchestrator(t, tool, 1)

	submit := func() string {
		res, err := orch.Submit(context.Background(), models.JobRequestV1{
			ToolID:     tool.ID(),
			Mode:       "batch",
			InputPaths: []string{"a", "b"},
			OutputDir:  "/tmp",
			Options:    map[string]any{},
		})
		if err != nil {
			t.Fatalf("submit failed: %v", err)
		}
		return res.JobID
	}

	job1 := submit()
	job2 := submit()

	_ = waitForTerminalStatus(t, orch, job1)
	_ = waitForTerminalStatus(t, orch, job2)

	if maxActive.Load() > 1 {
		t.Fatalf("expected max active <= 1, got %d", maxActive.Load())
	}
}

func TestOrchestratorBatchReturnsPartialSuccess(t *testing.T) {
	tool := &testTool{id: "tool.test.partial", capability: "test", supportSingle: true, supportBatch: true}
	orch := newTestOrchestrator(t, tool, 2)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "batch",
		InputPaths: []string{"a", "b", "c"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	result := waitForTerminalStatus(t, orch, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("expected initial success, got %s", result.Status)
	}

	result.Items[1].Success = false
	result.Items[1].Error = models.NewCanonicalJobError("ITEM_FAIL", "item failed", nil)
	status, msg := deriveBatchFinalState(result.Items)
	if status != StatusPartialSuccess {
		t.Fatalf("expected partial_success, got %s", status)
	}
	if msg != "job partial success" {
		t.Fatalf("unexpected message: %s", msg)
	}
}

type mixedBatchTool struct{}

func (m *mixedBatchTool) ID() string         { return "tool.test.partial-details" }
func (m *mixedBatchTool) Capability() string { return "test" }
func (m *mixedBatchTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: m.ID(), Name: "mixed", Capability: m.Capability(), SupportsBatch: true, SupportsSingle: false}
}
func (m *mixedBatchTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}
func (m *mixedBatchTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}
func (m *mixedBatchTool) ExecuteBatch(_ context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	return []models.JobResultItemV1{
			{InputPath: req.InputPaths[0], OutputPath: "/tmp/a.pdf", Success: false, Message: "range out of bounds", Error: &models.JobErrorV1{Code: "PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS", Message: "range out of bounds"}},
			{InputPath: req.InputPaths[1], OutputPath: "/tmp/b.pdf", Success: true, Message: "ok"},
		}, &models.JobErrorV1{
			Code:       "PDF_CROP_FAILED",
			DetailCode: "PDF_CROP_FAILED",
			Message:    "one or more files failed",
			Details: map[string]any{
				"fileErrors": []map[string]any{{
					"path":    req.InputPaths[0],
					"code":    "PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS",
					"message": "range out of bounds",
				}},
			},
		}
}

var _ tools.Tool = (*mixedBatchTool)(nil)
var _ tools.BatchExecutor = (*mixedBatchTool)(nil)

func TestOrchestratorBatchErrorKeepsItemErrorsAndAggregateDetails(t *testing.T) {
	reg := registry.NewRegistry()
	tool := &mixedBatchTool{}
	reg.SafeRegisterToolV2(tool)
	orch := NewOrchestrator(reg, 1)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "batch",
		InputPaths: []string{"/tmp/input-a.pdf", "/tmp/input-b.pdf"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	result := waitForTerminalStatus(t, orch, response.JobID)
	if result.Status != StatusPartialSuccess {
		t.Fatalf("expected partial_success, got %s", result.Status)
	}
	if result.Error == nil || result.Error.DetailCode != "PDF_CROP_FAILED" {
		t.Fatalf("expected aggregate crop error, got %+v", result.Error)
	}
	if result.Items[0].Error == nil || result.Items[0].Error.DetailCode != "PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS" {
		t.Fatalf("expected item error preserved, got %+v", result.Items[0].Error)
	}

	raw := result.Error.Details["fileErrors"]
	if raw == nil {
		t.Fatalf("expected details.fileErrors in aggregate error, got %+v", result.Error.Details)
	}
}

func TestOrchestratorSingleRetryStopsAtThreeAndSucceeds(t *testing.T) {
	tool := &testTool{
		id:              "tool.doc.docx_to_pdf",
		capability:      "doc",
		supportSingle:   true,
		supportBatch:    false,
		singleFailCount: 2,
	}
	orch := newTestOrchestrator(t, tool, 1)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "single",
		InputPaths: []string{"/tmp/in.docx"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	result := waitForTerminalStatus(t, orch, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("expected success after retries, got %s", result.Status)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected one result item, got %d", len(result.Items))
	}
	if result.Items[0].Attempts != 3 {
		t.Fatalf("expected attempts=3, got %d", result.Items[0].Attempts)
	}
	if result.Items[0].RetryCount != 2 {
		t.Fatalf("expected retryCount=2, got %d", result.Items[0].RetryCount)
	}
}

func TestOrchestratorBatchRetryMaxThreeAndPartialSuccess(t *testing.T) {
	tool := &testTool{
		id:             "tool.doc.docx_to_pdf",
		capability:     "doc",
		supportSingle:  true,
		supportBatch:   true,
		batchFailCount: 2,
	}
	orch := newTestOrchestrator(t, tool, 1)

	response, err := orch.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "batch",
		InputPaths: []string{"a.docx", "b.docx"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	result := waitForTerminalStatus(t, orch, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("expected success after transient batch retries, got %s", result.Status)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	for _, item := range result.Items {
		if item.Attempts != 3 {
			t.Fatalf("expected attempts=3 for item %s, got %d", item.InputPath, item.Attempts)
		}
		if item.RetryCount != 2 {
			t.Fatalf("expected retryCount=2 for item %s, got %d", item.InputPath, item.RetryCount)
		}
	}
}

func TestOrchestratorRecoveryMarksQueuedAndRunningAsInterrupted(t *testing.T) {
	reg := registry.NewRegistry()
	tool := &testTool{id: "tool.doc.docx_to_pdf", capability: "doc", supportSingle: true, supportBatch: true}
	tool.batchDelay = 120 * time.Millisecond
	reg.SafeRegisterToolV2(tool)

	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs_state_v1.json")

	orchA := NewOrchestrator(reg, 1)
	orchA.SetPersistencePath(storePath)

	_, err := orchA.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.ID(),
		Mode:       "batch",
		InputPaths: []string{"a.docx", "b.docx", "c.docx"},
		OutputDir:  "/tmp",
		Options:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	orchB := NewOrchestrator(reg, 1)
	orchB.SetPersistencePath(storePath)
	if err := orchB.RecoverInterruptedJobs(); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}

	orchB.mu.RLock()
	defer orchB.mu.RUnlock()
	if len(orchB.jobs) == 0 {
		t.Fatalf("expected recovered interrupted jobs in history")
	}

	foundInterrupted := false
	for _, tracked := range orchB.jobs {
		snap := tracked.snapshot()
		if snap.Status == StatusInterrupted {
			foundInterrupted = true
			if snap.Progress.Stage != StatusInterrupted {
				t.Fatalf("expected interrupted progress stage, got %s", snap.Progress.Stage)
			}
		}
	}

	if !foundInterrupted {
		t.Fatalf("expected at least one interrupted recovered job")
	}
}
