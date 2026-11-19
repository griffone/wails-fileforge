package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/tools"
)

// engineFakeTool is a configurable tool used by the engine tests. Single and
// batch hooks are independent so a test only implements the mode it exercises.
type engineFakeTool struct {
	id          string
	validateErr *models.JobErrorV1
	single      func(context.Context, models.JobRequestV1, func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1)
	batch       func(context.Context, models.JobRequestV1, func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1)
}

func (t *engineFakeTool) ID() string {
	return t.id
}

func (t *engineFakeTool) Capability() string {
	return t.id
}

func (t *engineFakeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.id}
}

func (t *engineFakeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *engineFakeTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return t.validateErr
}

func (t *engineFakeTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.single(ctx, req, onProgress)
}

func (t *engineFakeTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	return t.batch(ctx, req, onProgress)
}

// releaseGate is a close-once signal used to unblock fake tool executions.
type releaseGate struct {
	once sync.Once
	ch   chan struct{}
}

func newReleaseGate() *releaseGate {
	return &releaseGate{ch: make(chan struct{})}
}

func (g *releaseGate) close() {
	g.once.Do(func() { close(g.ch) })
}

func closeAllGates(gates map[string]*releaseGate) {
	for _, gate := range gates {
		gate.close()
	}
}

func newBlockingEngineTool(id string, started chan<- string, gates map[string]*releaseGate) *engineFakeTool {
	return &engineFakeTool{
		id: id,
		single: func(_ context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			path := firstEngineInput(req)
			started <- path
			if gate := gates[path]; gate != nil {
				<-gate.ch
			}

			output := path + ".out"
			return models.JobResultItemV1{
				InputPath:  path,
				OutputPath: output,
				Outputs:    []string{output},
				Success:    true,
				Message:    "done",
			}, nil
		},
	}
}

func firstEngineInput(req models.JobRequestV1) string {
	if len(req.InputPaths) == 0 {
		return ""
	}

	return req.InputPaths[0]
}

func newEngineResolver(t *testing.T, registered ...tools.Tool) *registry.Registry {
	t.Helper()

	reg := registry.NewRegistry()
	for _, tool := range registered {
		if err := reg.RegisterToolV2(tool); err != nil {
			t.Fatalf("register tool %q: %v", tool.ID(), err)
		}
	}

	return reg
}

func testEngineLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestEngine(t *testing.T, resolver ToolResolver, opts Options) *Engine {
	t.Helper()

	if opts.Logger == nil {
		opts.Logger = testEngineLogger()
	}
	engine := NewEngine(resolver, opts)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = engine.Shutdown(ctx)
	})

	return engine
}

func submitTestJob(t *testing.T, engine *Engine, req models.JobRequestV1) models.RunJobResponseV1 {
	t.Helper()

	response, err := engine.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("submit job: %v", err)
	}
	if !response.Success {
		t.Fatalf("job rejected: %+v", response)
	}

	return response
}

func submitTestJobWith(t *testing.T, engine *Engine, req models.JobRequestV1, opts SubmitOptions) models.RunJobResponseV1 {
	t.Helper()

	response, err := engine.SubmitWithOptions(context.Background(), req, opts)
	if err != nil {
		t.Fatalf("submit job with options: %v", err)
	}
	if !response.Success {
		t.Fatalf("job rejected: %+v", response)
	}

	return response
}

func awaitEngineStart(t *testing.T, started <-chan string) string {
	t.Helper()

	select {
	case path := <-started:
		return path
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a job to start")
	}

	return ""
}

func assertNoEngineStart(t *testing.T, started <-chan string, window time.Duration) {
	t.Helper()

	select {
	case path := <-started:
		t.Fatalf("unexpected job start for %q", path)
	case <-time.After(window):
	}
}

func awaitEngineJob(t *testing.T, engine *Engine, jobID string) models.JobResultV1 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := engine.Wait(ctx, jobID)
	if err != nil {
		t.Fatalf("wait for job %s: %v", jobID, err)
	}

	return result
}

func engineInput(name string) string {
	return "/tmp/fileforge-engine/" + name
}

func TestDefaultLimitsInvariants(t *testing.T) {
	limits := DefaultLimits()
	if limits.Total < 2 || limits.Total > 6 {
		t.Fatalf("Total = %d, want [2,6]", limits.Total)
	}
	if limits.CPU < 1 || limits.CPU > 4 {
		t.Fatalf("CPU = %d, want [1,4]", limits.CPU)
	}
	if limits.External != 1 {
		t.Fatalf("External = %d, want 1", limits.External)
	}
	if limits.IO != 2 {
		t.Fatalf("IO = %d, want 2", limits.IO)
	}
	if limits.Total < max(limits.CPU, limits.External, limits.IO) {
		t.Fatalf("Total = %d must cover every class: %+v", limits.Total, limits)
	}
}

func TestClassForToolMapping(t *testing.T) {
	tests := []struct {
		name   string
		toolID string
		want   Class
	}{
		{name: "image convert", toolID: "tool.image.convert", want: ClassCPU},
		{name: "image ocr", toolID: "tool.image.ocr", want: ClassCPU},
		{name: "pdf merge", toolID: "tool.pdf.merge", want: ClassCPU},
		{name: "pdf split", toolID: "tool.pdf.split", want: ClassCPU},
		{name: "pdf crop", toolID: "tool.pdf.crop", want: ClassCPU},
		{name: "pdf optimize", toolID: "tool.pdf.optimize", want: ClassCPU},
		{name: "pdf images", toolID: "tool.pdf.images", want: ClassCPU},
		{name: "pdf ocr", toolID: "tool.pdf.ocr", want: ClassExternal},
		{name: "pdf to docx", toolID: "tool.pdf.to_docx", want: ClassExternal},
		{name: "video convert", toolID: "tool.video.convert", want: ClassExternal},
		{name: "doc md to pdf", toolID: "tool.doc.md_to_pdf", want: ClassExternal},
		{name: "unknown", toolID: "tool.unknown", want: ClassCPU},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassForTool(tt.toolID); got != tt.want {
				t.Fatalf("ClassForTool(%q) = %q, want %q", tt.toolID, got, tt.want)
			}
		})
	}
}

func TestEnginePriorityOrder(t *testing.T) {
	started := make(chan string, 8)
	gates := map[string]*releaseGate{
		engineInput("a"):  newReleaseGate(),
		engineInput("b"):  newReleaseGate(),
		engineInput("c1"): newReleaseGate(),
		engineInput("c2"): newReleaseGate(),
	}
	t.Cleanup(func() { closeAllGates(gates) })

	engine := newTestEngine(t, newEngineResolver(t, newBlockingEngineTool("tool.fake.priority", started, gates)), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	running := submitTestJobWith(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.priority",
		Mode:       "single",
		InputPaths: []string{engineInput("a")},
	}, SubmitOptions{Kind: "tool", Priority: PriorityAutomation, Class: ClassCPU})
	if path := awaitEngineStart(t, started); path != engineInput("a") {
		t.Fatalf("first started job = %q, want a", path)
	}

	lowFirst := submitTestJobWith(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.priority",
		Mode:       "single",
		InputPaths: []string{engineInput("c1")},
	}, SubmitOptions{Kind: "tool", Priority: PriorityPreview, Class: ClassCPU})
	lowSecond := submitTestJobWith(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.priority",
		Mode:       "single",
		InputPaths: []string{engineInput("c2")},
	}, SubmitOptions{Kind: "tool", Priority: PriorityPreview, Class: ClassCPU})

	high := submitTestJobWith(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.priority",
		Mode:       "single",
		InputPaths: []string{engineInput("b")},
	}, SubmitOptions{Kind: "tool", Priority: PriorityInteractive, Class: ClassCPU})

	gates[engineInput("a")].close()

	if path := awaitEngineStart(t, started); path != engineInput("b") {
		t.Fatalf("second started job = %q, want the interactive job b", path)
	}

	gates[engineInput("b")].close()

	if path := awaitEngineStart(t, started); path != engineInput("c1") {
		t.Fatalf("third started job = %q, want the first preview job c1", path)
	}
	gates[engineInput("c1")].close()

	if path := awaitEngineStart(t, started); path != engineInput("c2") {
		t.Fatalf("fourth started job = %q, want the second preview job c2", path)
	}
	gates[engineInput("c2")].close()

	for _, jobID := range []string{running.JobID, high.JobID, lowFirst.JobID, lowSecond.JobID} {
		if result := awaitEngineJob(t, engine, jobID); result.Status != StatusSuccess {
			t.Fatalf("job %s status = %s, want success", jobID, result.Status)
		}
	}
}

func TestEngineClassLimitsAndNoHeadOfLineBlocking(t *testing.T) {
	started := make(chan string, 8)
	gates := map[string]*releaseGate{
		engineInput("cpu-a"): newReleaseGate(),
		engineInput("ext-b"): newReleaseGate(),
		engineInput("cpu-c"): newReleaseGate(),
		engineInput("io-d"):  newReleaseGate(),
	}
	t.Cleanup(func() { closeAllGates(gates) })

	engine := newTestEngine(t, newEngineResolver(t, newBlockingEngineTool("tool.fake.classes", started, gates)), Options{
		Limits: Limits{Total: 2, CPU: 1, External: 1, IO: 1},
	})

	cpuJob := submitTestJobWith(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.classes",
		Mode:       "single",
		InputPaths: []string{engineInput("cpu-a")},
	}, SubmitOptions{Kind: "tool", Priority: PriorityInteractive, Class: ClassCPU})
	externalJob := submitTestJobWith(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.classes",
		Mode:       "single",
		InputPaths: []string{engineInput("ext-b")},
	}, SubmitOptions{Kind: "tool", Priority: PriorityInteractive, Class: ClassExternal})

	got := map[string]bool{
		awaitEngineStart(t, started): true,
		awaitEngineStart(t, started): true,
	}
	if !got[engineInput("cpu-a")] || !got[engineInput("ext-b")] {
		t.Fatalf("expected the CPU job and the external job to run concurrently, got %v", got)
	}

	blockedCPU := submitTestJobWith(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.classes",
		Mode:       "single",
		InputPaths: []string{engineInput("cpu-c")},
	}, SubmitOptions{Kind: "tool", Priority: PriorityInteractive, Class: ClassCPU})
	assertNoEngineStart(t, started, 150*time.Millisecond)

	gates[engineInput("ext-b")].close()

	// The preview IO job is behind the interactive CPU job in the queue and has
	// a lower priority, but it must run while the CPU class is saturated.
	lowPriorityIO := submitTestJobWith(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.classes",
		Mode:       "single",
		InputPaths: []string{engineInput("io-d")},
	}, SubmitOptions{Kind: "tool", Priority: PriorityPreview, Class: ClassIO})

	if path := awaitEngineStart(t, started); path != engineInput("io-d") {
		t.Fatalf("started job = %q, want io-d", path)
	}
	assertNoEngineStart(t, started, 150*time.Millisecond)

	gates[engineInput("cpu-a")].close()

	if path := awaitEngineStart(t, started); path != engineInput("cpu-c") {
		t.Fatalf("started job = %q, want cpu-c", path)
	}
	gates[engineInput("cpu-c")].close()
	gates[engineInput("io-d")].close()

	for _, jobID := range []string{cpuJob.JobID, externalJob.JobID, blockedCPU.JobID, lowPriorityIO.JobID} {
		if result := awaitEngineJob(t, engine, jobID); result.Status != StatusSuccess {
			t.Fatalf("job %s status = %s, want success", jobID, result.Status)
		}
	}
}

func TestEngineQueueFullReturnsSentinel(t *testing.T) {
	started := make(chan string, 4)
	gates := map[string]*releaseGate{
		engineInput("a"): newReleaseGate(),
		engineInput("b"): newReleaseGate(),
	}
	t.Cleanup(func() { closeAllGates(gates) })

	engine := newTestEngine(t, newEngineResolver(t, newBlockingEngineTool("tool.fake.queue", started, gates)), Options{
		Limits:   Limits{Total: 1, CPU: 1, External: 1, IO: 1},
		MaxQueue: 1,
	})

	running := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.queue",
		Mode:       "single",
		InputPaths: []string{engineInput("a")},
	})
	awaitEngineStart(t, started)

	queued := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.queue",
		Mode:       "single",
		InputPaths: []string{engineInput("b")},
	})

	response, err := engine.Submit(context.Background(), models.JobRequestV1{
		ToolID:     "tool.fake.queue",
		Mode:       "single",
		InputPaths: []string{engineInput("c")},
	})
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("Submit error = %v, want ErrQueueFull", err)
	}
	if response.JobID != "" {
		t.Fatalf("rejected submission returned job id %q", response.JobID)
	}

	gates[engineInput("a")].close()
	if path := awaitEngineStart(t, started); path != engineInput("b") {
		t.Fatalf("started job = %q, want b", path)
	}
	gates[engineInput("b")].close()

	if result := awaitEngineJob(t, engine, running.JobID); result.Status != StatusSuccess {
		t.Fatalf("running job status = %s, want success", result.Status)
	}
	if result := awaitEngineJob(t, engine, queued.JobID); result.Status != StatusSuccess {
		t.Fatalf("queued job status = %s, want success", result.Status)
	}
}

func TestEngineCancelQueuedJobNeverExecutes(t *testing.T) {
	started := make(chan string, 4)
	gates := map[string]*releaseGate{
		engineInput("a"): newReleaseGate(),
		engineInput("b"): newReleaseGate(),
	}
	t.Cleanup(func() { closeAllGates(gates) })

	engine := newTestEngine(t, newEngineResolver(t, newBlockingEngineTool("tool.fake.cancel.queue", started, gates)), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	running := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.cancel.queue",
		Mode:       "single",
		InputPaths: []string{engineInput("a")},
	})
	awaitEngineStart(t, started)

	queued := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     "tool.fake.cancel.queue",
		Mode:       "single",
		InputPaths: []string{engineInput("b")},
	})

	if err := engine.Cancel(queued.JobID); err != nil {
		t.Fatalf("cancel queued job: %v", err)
	}

	result := awaitEngineJob(t, engine, queued.JobID)
	if result.Status != StatusCancelled {
		t.Fatalf("queued job status = %s, want cancelled", result.Status)
	}
	if result.Error == nil || result.Error.DetailCode != "JOB_CANCELLED" {
		t.Fatalf("expected JOB_CANCELLED error, got %+v", result.Error)
	}

	gates[engineInput("a")].close()
	if result := awaitEngineJob(t, engine, running.JobID); result.Status != StatusSuccess {
		t.Fatalf("running job status = %s, want success", result.Status)
	}
	assertNoEngineStart(t, started, 150*time.Millisecond)
}

func TestEngineCancelRunningJobIsPrompt(t *testing.T) {
	started := make(chan string, 1)
	tool := &engineFakeTool{
		id: "tool.image.convert",
		single: func(ctx context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			path := firstEngineInput(req)
			started <- path
			<-ctx.Done()

			item := models.JobResultItemV1{InputPath: path, Success: false, Message: "cancelled"}
			return item, models.NewCanonicalJobError("IMAGE_SINGLE_EXECUTION", ctx.Err().Error(), nil)
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{engineInput("cancel-running")},
	})
	awaitEngineStart(t, started)

	if err := engine.Cancel(response.JobID); err != nil {
		t.Fatalf("cancel running job: %v", err)
	}

	result := awaitEngineJob(t, engine, response.JobID)
	if result.Status != StatusCancelled {
		t.Fatalf("running job status = %s, want cancelled", result.Status)
	}
}

func TestEnginePanicRecovery(t *testing.T) {
	panicInput := engineInput("panic")
	tool := &engineFakeTool{
		id: "tool.fake.panic",
		single: func(_ context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			path := firstEngineInput(req)
			if path == panicInput {
				panic("boom")
			}

			return models.JobResultItemV1{InputPath: path, Success: true, Message: "ok"}, nil
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	panicking := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{panicInput},
	})

	result := awaitEngineJob(t, engine, panicking.JobID)
	if result.Status != StatusFailed {
		t.Fatalf("panicking job status = %s, want failed", result.Status)
	}
	if result.Error == nil || result.Error.DetailCode != "PANIC" {
		t.Fatalf("expected PANIC detail code, got %+v", result.Error)
	}

	recovered := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{engineInput("after-panic")},
	})
	if result := awaitEngineJob(t, engine, recovered.JobID); result.Status != StatusSuccess {
		t.Fatalf("follow-up job status = %s, want success", result.Status)
	}
}

func TestEnginePersistenceRecoveryMarksInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "jobs.json")
	started := make(chan string, 2)
	gates := map[string]*releaseGate{engineInput("blocked"): newReleaseGate()}
	t.Cleanup(func() { closeAllGates(gates) })

	resolver := newEngineResolver(t, newBlockingEngineTool("tool.fake.recovery", started, gates))
	opts := Options{
		Limits:           Limits{Total: 1, CPU: 1, External: 1, IO: 1},
		PersistencePath:  path,
		Debounce:         10 * time.Millisecond,
		ProgressInterval: 5 * time.Millisecond,
		Logger:           testEngineLogger(),
	}

	first := NewEngine(resolver, opts)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = first.Shutdown(ctx)
	})

	response := submitTestJob(t, first, models.JobRequestV1{
		ToolID:     "tool.fake.recovery",
		Mode:       "single",
		InputPaths: []string{engineInput("blocked")},
	})
	if foundPath := awaitEngineStart(t, started); foundPath != engineInput("blocked") {
		t.Fatalf("started job = %q, want blocked", foundPath)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := first.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v, want context.DeadlineExceeded", err)
	}

	second := NewEngine(resolver, opts)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = second.Shutdown(ctx)
	})

	if err := second.Recover(context.Background()); err != nil {
		t.Fatalf("recover jobs: %v", err)
	}

	recovered, found := second.Get(response.JobID)
	if !found {
		t.Fatalf("expected recovered job %s", response.JobID)
	}
	if recovered.Status != StatusInterrupted || recovered.Success {
		t.Fatalf("recovered job = %+v, want interrupted", recovered)
	}
	if recovered.Message != "job interrupted after restart" {
		t.Fatalf("recovered message = %q", recovered.Message)
	}
	if recovered.EndedAt == 0 || recovered.Progress.Stage != StatusInterrupted {
		t.Fatalf("expected interrupted terminal fields, got %+v", recovered)
	}
	if recovered.Error == nil || recovered.Error.DetailCode != "JOB_INTERRUPTED" {
		t.Fatalf("expected JOB_INTERRUPTED error, got %+v", recovered.Error)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted jobs: %v", err)
	}
	var persisted []models.JobResultV1
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("decode persisted jobs: %v", err)
	}
	persistedStatus := ""
	for _, job := range persisted {
		if job.JobID == response.JobID {
			persistedStatus = job.Status
			break
		}
	}
	if persistedStatus != StatusInterrupted {
		t.Fatalf("persisted status = %q, want interrupted", persistedStatus)
	}

	gates[engineInput("blocked")].close()
}

func TestEngineSetPersistencePathEmptyIsNoOp(t *testing.T) {
	tool := &engineFakeTool{
		id: "tool.fake.persist.empty",
		single: func(_ context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			return models.JobResultItemV1{InputPath: firstEngineInput(req), Success: true, Message: "done"}, nil
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	if err := engine.SetPersistencePath("   "); err != nil {
		t.Fatalf("SetPersistencePath with an empty path: %v", err)
	}
	engine.persistMu.Lock()
	store := engine.store
	path := engine.opts.PersistencePath
	engine.persistMu.Unlock()
	if store != nil || path != "" {
		t.Fatalf("empty path configured persistence: store=%v path=%q", store, path)
	}

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{engineInput("no-persistence")},
	})
	if result := awaitEngineJob(t, engine, response.JobID); result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success", result.Status)
	}
}

func TestEngineSetPersistencePathPersistsExistingJobs(t *testing.T) {
	tool := &engineFakeTool{
		id: "tool.fake.persist.switch",
		single: func(_ context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			return models.JobResultItemV1{InputPath: firstEngineInput(req), Success: true, Message: "done"}, nil
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{engineInput("persist-switch")},
	})
	if result := awaitEngineJob(t, engine, response.JobID); result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success", result.Status)
	}

	firstPath := filepath.Join(t.TempDir(), "first", "jobs.json")
	if err := engine.SetPersistencePath(firstPath); err != nil {
		t.Fatalf("set first persistence path: %v", err)
	}
	persisted := readEnginePersistedJobs(t, firstPath)
	if len(persisted) != 1 || persisted[0].JobID != response.JobID {
		t.Fatalf("first snapshot = %+v, want job %s", persisted, response.JobID)
	}

	secondPath := filepath.Join(t.TempDir(), "second", "jobs.json")
	if err := engine.SetPersistencePath(secondPath); err != nil {
		t.Fatalf("set second persistence path: %v", err)
	}
	persisted = readEnginePersistedJobs(t, secondPath)
	if len(persisted) != 1 || persisted[0].JobID != response.JobID {
		t.Fatalf("second snapshot = %+v, want job %s", persisted, response.JobID)
	}
}

func readEnginePersistedJobs(t *testing.T, path string) []models.JobResultV1 {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted jobs: %v", err)
	}

	var persisted []models.JobResultV1
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("decode persisted jobs: %v", err)
	}

	return persisted
}

func TestEngineSubmitRejectsInvalidRequest(t *testing.T) {
	validationErr := models.NewCanonicalJobError("VALIDATION_INVALID_INPUT", "input missing", nil)
	tool := &engineFakeTool{
		id:          "tool.fake.validation",
		validateErr: validationErr,
		single: func(_ context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			return models.JobResultItemV1{InputPath: firstEngineInput(req), Success: true}, nil
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	response, err := engine.Submit(context.Background(), models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{engineInput("invalid")},
	})
	if err != nil {
		t.Fatalf("Submit returned error: %v", err)
	}
	if response.Success || response.Status != StatusFailed {
		t.Fatalf("response = %+v, want a failed validation response", response)
	}
	if response.Error == nil || response.Error.DetailCode != "VALIDATION_INVALID_INPUT" {
		t.Fatalf("validation error = %+v", response.Error)
	}
	if jobs := engine.List(0); len(jobs) != 0 {
		t.Fatalf("rejected job was tracked: %+v", jobs)
	}
}

func TestEngineSingleRetry(t *testing.T) {
	var attempts atomic.Int32
	tool := &engineFakeTool{
		id: "tool.image.convert",
		single: func(_ context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			path := firstEngineInput(req)
			item := models.JobResultItemV1{InputPath: path}
			if attempts.Add(1) == 1 {
				jobErr := models.NewCanonicalJobError("IMAGE_SINGLE_EXECUTION", "transient failure", nil)
				item.Success = false
				item.Message = jobErr.Message
				item.Error = jobErr
				return item, jobErr
			}

			item.Success = true
			item.Message = "recovered"
			item.OutputPath = path + ".out"
			item.Outputs = []string{item.OutputPath}
			return item, nil
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{engineInput("retry")},
	})

	result := awaitEngineJob(t, engine, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success (%s)", result.Status, result.Message)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	if result.Items[0].Attempts != 2 || result.Items[0].RetryCount != 1 {
		t.Fatalf("item attempts = %d retryCount = %d, want 2/1", result.Items[0].Attempts, result.Items[0].RetryCount)
	}
	if attempts.Load() != 2 {
		t.Fatalf("execute calls = %d, want 2", attempts.Load())
	}
}

func TestEngineBatchRetriesOnlyFailedRetryableItems(t *testing.T) {
	okOne := engineInput("one")
	duplicate := engineInput("duplicate")
	okThree := engineInput("three")
	inputs := []string{okOne, duplicate, duplicate, okThree}

	var mu sync.Mutex
	var calls [][]string

	tool := &engineFakeTool{
		id: "tool.image.crop",
		batch: func(_ context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
			mu.Lock()
			calls = append(calls, append([]string(nil), req.InputPaths...))
			call := len(calls)
			mu.Unlock()

			items := make([]models.JobResultItemV1, 0, len(req.InputPaths))
			var batchErr *models.JobErrorV1
			for index, path := range req.InputPaths {
				if onProgress != nil {
					onProgress(models.JobProgressV1{Current: index + 1, Total: len(req.InputPaths), Stage: "running", Message: "processing"})
				}

				if call == 1 && path == duplicate {
					jobErr := models.NewCanonicalJobError("IMAGE_CROP_BATCH_ITEM", "transient failure", nil)
					batchErr = jobErr
					items = append(items, models.JobResultItemV1{
						InputPath: path,
						Success:   false,
						Message:   jobErr.Message,
						Error:     jobErr,
					})
					continue
				}

				output := path + ".out"
				items = append(items, models.JobResultItemV1{
					InputPath:  path,
					OutputPath: output,
					Outputs:    []string{output},
					Success:    true,
					Message:    "cropped",
				})
			}

			return items, batchErr
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "batch",
		InputPaths: inputs,
	})

	result := awaitEngineJob(t, engine, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success (%s)", result.Status, result.Message)
	}

	mu.Lock()
	copiedCalls := make([][]string, len(calls))
	for index, call := range calls {
		copiedCalls[index] = append([]string(nil), call...)
	}
	mu.Unlock()

	if len(copiedCalls) != 2 {
		t.Fatalf("ExecuteBatch calls = %d, want 2: %v", len(copiedCalls), copiedCalls)
	}
	if len(copiedCalls[0]) != len(inputs) {
		t.Fatalf("first call inputs = %v, want %v", copiedCalls[0], inputs)
	}
	if len(copiedCalls[1]) != 1 || copiedCalls[1][0] != duplicate {
		t.Fatalf("retry call inputs = %v, want [%s]", copiedCalls[1], duplicate)
	}

	if len(result.Items) != len(inputs) {
		t.Fatalf("items = %d, want %d", len(result.Items), len(inputs))
	}
	wantAttempts := []int{1, 2, 2, 1}
	for index, want := range wantAttempts {
		item := result.Items[index]
		if !item.Success {
			t.Fatalf("item %d failed: %+v", index, item)
		}
		if item.InputPath != inputs[index] {
			t.Fatalf("item %d input = %q, want %q", index, item.InputPath, inputs[index])
		}
		if item.Attempts != want || item.RetryCount != want-1 {
			t.Fatalf("item %d attempts/retryCount = %d/%d, want %d/%d", index, item.Attempts, item.RetryCount, want, want-1)
		}
	}
}

type recordingEngineEmitter struct {
	mu       sync.Mutex
	progress []models.JobProgressEventV1
	states   []models.JobResultV1
}

func (e *recordingEngineEmitter) JobProgress(event models.JobProgressEventV1) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.progress = append(e.progress, event)
}

func (e *recordingEngineEmitter) JobState(result models.JobResultV1) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.states = append(e.states, result)
}

func (e *recordingEngineEmitter) snapshots() ([]models.JobProgressEventV1, []models.JobResultV1) {
	e.mu.Lock()
	defer e.mu.Unlock()

	progressEvents := append([]models.JobProgressEventV1(nil), e.progress...)
	stateEvents := append([]models.JobResultV1(nil), e.states...)

	return progressEvents, stateEvents
}

func TestEngineProgressCoalescingEmitsTerminalState(t *testing.T) {
	const reportCount = 40

	emitter := &recordingEngineEmitter{}
	tool := &engineFakeTool{
		id: "tool.fake.progress",
		single: func(_ context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			path := firstEngineInput(req)
			for current := 1; current <= reportCount; current++ {
				onProgress(models.JobProgressV1{
					Current: current,
					Total:   reportCount,
					Stage:   "running",
					Message: "tick",
				})
			}

			return models.JobResultItemV1{InputPath: path, Success: true, Message: "done"}, nil
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits:           Limits{Total: 1, CPU: 1, External: 1, IO: 1},
		Emitter:          emitter,
		ProgressInterval: time.Hour,
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{engineInput("progress")},
	})

	result := awaitEngineJob(t, engine, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success", result.Status)
	}

	progressEvents, stateEvents := emitter.snapshots()
	if len(progressEvents) == 0 {
		t.Fatal("expected at least the terminal progress event")
	}
	if len(progressEvents) >= reportCount {
		t.Fatalf("progress events = %d, want fewer than %d reports", len(progressEvents), reportCount)
	}
	if terminal := progressEvents[len(progressEvents)-1]; terminal.Status != StatusSuccess {
		t.Fatalf("last progress event status = %q, want success", terminal.Status)
	}

	if len(stateEvents) == 0 {
		t.Fatal("expected a terminal state event")
	}
	if terminal := stateEvents[len(stateEvents)-1]; terminal.Status != StatusSuccess {
		t.Fatalf("last state event status = %q, want success", terminal.Status)
	}
}

func TestEngineShutdownIdempotentAndNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	tool := &engineFakeTool{
		id: "tool.fake.shutdown",
		single: func(_ context.Context, req models.JobRequestV1, _ func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
			return models.JobResultItemV1{InputPath: firstEngineInput(req), Success: true, Message: "done"}, nil
		},
	}

	engine := NewEngine(newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
		Logger: testEngineLogger(),
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{engineInput("shutdown")},
	})
	if result := awaitEngineJob(t, engine, response.JobID); result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success", result.Status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Shutdown(ctx); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	if err := engine.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		after := runtime.NumGoroutine()
		if after <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines before=%d after=%d, possible leak", before, after)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEngineOnJobCompleteRunsOnceForTerminalJob(t *testing.T) {
	tests := []struct {
		name       string
		fail       bool
		wantStatus string
	}{
		{name: "success", wantStatus: StatusSuccess},
		{name: "failure", fail: true, wantStatus: StatusFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &fakeSingleFileTool{
				id:         "tool.fake.hook." + tt.name,
				outputPath: filepath.Join(t.TempDir(), "output.pdf"),
				fail:       tt.fail,
			}
			completions := make(chan models.JobResultV1, 4)
			engine := newTestEngine(t, newEngineResolver(t, tool), Options{
				Limits:        Limits{Total: 1, CPU: 1, External: 1, IO: 1},
				OnJobComplete: func(result models.JobResultV1) { completions <- result },
			})

			response := submitTestJob(t, engine, models.JobRequestV1{
				ToolID:     tool.id,
				Mode:       "single",
				InputPaths: []string{"/tmp/fileforge-hook-input.pdf"},
			})
			result := awaitEngineJob(t, engine, response.JobID)
			if result.Status != tt.wantStatus {
				t.Fatalf("job status = %s, want %s", result.Status, tt.wantStatus)
			}

			results := awaitJobCompletions(t, completions, 1)
			completion := results[0]
			if completion.JobID != response.JobID || completion.ToolID != tool.id {
				t.Fatalf("completion = %+v, want job %s of %s", completion, response.JobID, tool.id)
			}
			if completion.Status != tt.wantStatus {
				t.Fatalf("completion status = %s, want %s", completion.Status, tt.wantStatus)
			}
			assertNoExtraCompletion(t, completions, 100*time.Millisecond)
		})
	}
}

func TestEngineOnJobCompleteNeverRunsForQueuedOrRunningJobs(t *testing.T) {
	gate := newReleaseGate()
	started := make(chan struct{}, 2)
	tool := &fakeSingleFileTool{
		id:         "tool.fake.hook.pending",
		outputPath: filepath.Join(t.TempDir(), "output.pdf"),
		started:    started,
		waitFor:    gate.ch,
	}
	completions := make(chan models.JobResultV1, 4)
	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits:        Limits{Total: 1, CPU: 1, External: 1, IO: 1},
		OnJobComplete: func(result models.JobResultV1) { completions <- result },
	})
	t.Cleanup(gate.close)

	first := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-hook-a.pdf"},
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first job to start")
	}

	second := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-hook-b.pdf"},
	})

	select {
	case completion := <-completions:
		t.Fatalf("completion hook ran for a queued or running job: %+v", completion)
	case <-time.After(150 * time.Millisecond):
	}

	gate.close()

	for _, jobID := range []string{first.JobID, second.JobID} {
		if result := awaitEngineJob(t, engine, jobID); result.Status != StatusSuccess {
			t.Fatalf("job %s status = %s, want success", jobID, result.Status)
		}
	}

	results := awaitJobCompletions(t, completions, 2)
	seen := make(map[string]struct{}, len(results))
	for _, completion := range results {
		seen[completion.JobID] = struct{}{}
	}
	for _, jobID := range []string{first.JobID, second.JobID} {
		if _, found := seen[jobID]; !found {
			t.Fatalf("missing completion callback for job %s: %+v", jobID, results)
		}
	}
	assertNoExtraCompletion(t, completions, 100*time.Millisecond)
}

func TestEngineOnJobCompleteIgnoresRecoveredJobs(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	persisted := []models.JobResultV1{{
		JobID:   "job_interrupted",
		ToolID:  "tool.fake.recovery",
		Status:  StatusRunning,
		Message: "running",
	}}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode persisted jobs: %v", err)
	}
	if err := os.WriteFile(storePath, payload, 0o644); err != nil {
		t.Fatalf("write persisted jobs: %v", err)
	}

	completions := make(chan models.JobResultV1, 1)
	engine := newTestEngine(t, newEngineResolver(t), Options{
		PersistencePath: storePath,
		OnJobComplete:   func(result models.JobResultV1) { completions <- result },
	})

	if err := engine.Recover(context.Background()); err != nil {
		t.Fatalf("recover jobs: %v", err)
	}

	assertNoExtraCompletion(t, completions, 100*time.Millisecond)

	job, found := engine.Get("job_interrupted")
	if !found {
		t.Fatal("expected the recovered job to be available")
	}
	if job.Status != StatusInterrupted {
		t.Fatalf("recovered job status = %s, want interrupted", job.Status)
	}
}

func TestEngineListSortsNewestFirstAndAppliesLimit(t *testing.T) {
	engine := newTestEngine(t, newEngineResolver(t), Options{})
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_old", StartedAt: 100, Status: StatusSuccess})
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_tie_a", StartedAt: 200, Status: StatusSuccess})
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_tie_b", StartedAt: 200, Status: StatusSuccess})
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_stage", StartedAt: 150, Status: "ocr"})
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_new", StartedAt: 300, Status: StatusRunning})

	t.Run("default limit returns every job newest first", func(t *testing.T) {
		assertEngineJobOrder(t, engine.List(0), []string{"job_new", "job_tie_b", "job_tie_a", "job_stage", "job_old"})
	})

	t.Run("positive limit caps the list", func(t *testing.T) {
		assertEngineJobOrder(t, engine.List(2), []string{"job_new", "job_tie_b"})
	})
}

func TestEngineListReturnsEmptyNonNilSlice(t *testing.T) {
	engine := newTestEngine(t, newEngineResolver(t), Options{})

	jobs := engine.List(0)
	if jobs == nil {
		t.Fatal("expected an empty non-nil slice")
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no jobs, got %+v", jobs)
	}
}

func TestEngineTerminalRetentionKeepsNewestTerminalJobs(t *testing.T) {
	engine := newTestEngine(t, newEngineResolver(t), Options{})
	for index := 1; index <= retainedTerminalJobs+5; index++ {
		seedEngineJob(t, engine, models.JobResultV1{
			JobID:     engineJobIDForIndex(index),
			StartedAt: int64(index),
			Status:    StatusSuccess,
		})
	}
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_active_old", StartedAt: 1, Status: StatusRunning})
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_active_stage", StartedAt: 2, Status: "processing"})

	engine.pruneTerminalJobs()

	ids := engineTrackedJobIDs(engine)
	if len(ids) != retainedTerminalJobs+2 {
		t.Fatalf("expected %d retained jobs, got %d", retainedTerminalJobs+2, len(ids))
	}
	for index := 1; index <= 5; index++ {
		if _, found := ids[engineJobIDForIndex(index)]; found {
			t.Fatalf("expected the oldest terminal job %s to be pruned", engineJobIDForIndex(index))
		}
	}
	if _, found := ids[engineJobIDForIndex(retainedTerminalJobs+5)]; !found {
		t.Fatalf("expected the newest terminal job %s to be retained", engineJobIDForIndex(retainedTerminalJobs+5))
	}
	for _, jobID := range []string{"job_active_old", "job_active_stage"} {
		if _, found := ids[jobID]; !found {
			t.Fatalf("expected the non-terminal job %s to survive pruning", jobID)
		}
	}
}

func TestEngineSnapshotKeepsActiveAndNewestTerminalJobs(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "state", "jobs.json")
	engine := newTestEngine(t, newEngineResolver(t), Options{
		PersistencePath: storePath,
		Debounce:        10 * time.Millisecond,
	})

	for index := 1; index <= retainedTerminalJobs+5; index++ {
		seedEngineJob(t, engine, models.JobResultV1{
			JobID:     engineJobIDForIndex(index),
			StartedAt: int64(index),
			Status:    StatusSuccess,
		})
	}
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_queued", StartedAt: 500, Status: StatusQueued})
	seedEngineJob(t, engine, models.JobResultV1{JobID: "job_running", StartedAt: 501, Status: StatusRunning})

	engine.persistNow()

	persisted := readEnginePersistedJobs(t, storePath)
	if len(persisted) != retainedTerminalJobs+2 {
		t.Fatalf("expected %d persisted jobs, got %d", retainedTerminalJobs+2, len(persisted))
	}

	persistedIDs := make(map[string]struct{}, len(persisted))
	for _, job := range persisted {
		persistedIDs[job.JobID] = struct{}{}
	}
	for _, jobID := range []string{"job_queued", "job_running"} {
		if _, found := persistedIDs[jobID]; !found {
			t.Fatalf("expected the active job %s to be persisted", jobID)
		}
	}
	for index := 1; index <= 5; index++ {
		if _, found := persistedIDs[engineJobIDForIndex(index)]; found {
			t.Fatalf("expected the oldest terminal job %s to be excluded", engineJobIDForIndex(index))
		}
	}
	if _, found := persistedIDs[engineJobIDForIndex(retainedTerminalJobs+5)]; !found {
		t.Fatalf("expected the newest terminal job %s to be persisted", engineJobIDForIndex(retainedTerminalJobs+5))
	}

	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat persisted jobs: %v", err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o644 {
		t.Fatalf("expected 0644 permissions, got %o", permissions)
	}

	entries, err := os.ReadDir(filepath.Dir(storePath))
	if err != nil {
		t.Fatalf("read persistence directory: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(storePath) {
			t.Fatalf("expected no leftover temp files, found %s", entry.Name())
		}
	}
}

func TestEngineRecoverRestoresTerminalAndInterruptsActive(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	persisted := []models.JobResultV1{
		{
			JobID:     "job_terminal",
			ToolID:    "tool.fake.recovery",
			Success:   true,
			Message:   "job success",
			Status:    StatusSuccess,
			StartedAt: 100,
			EndedAt:   200,
		},
		{
			JobID:     "job_running",
			ToolID:    "tool.fake.recovery",
			Message:   "running",
			Status:    StatusRunning,
			Progress:  models.JobProgressV1{Current: 3, Total: 2, Stage: StatusRunning, Message: "running"},
			StartedAt: 300,
		},
		{
			JobID:     "job_queued",
			ToolID:    "tool.fake.recovery",
			Message:   "job queued",
			Status:    StatusQueued,
			StartedAt: 400,
		},
		{
			JobID:     "job_ocr",
			ToolID:    "tool.fake.recovery",
			Message:   "ocr in progress",
			Status:    "ocr",
			Progress:  models.JobProgressV1{Current: 1, Total: 4, Stage: "ocr"},
			StartedAt: 500,
		},
	}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode persisted jobs: %v", err)
	}
	if err := os.WriteFile(storePath, payload, 0o644); err != nil {
		t.Fatalf("write persisted jobs: %v", err)
	}

	engine := newTestEngine(t, newEngineResolver(t), Options{PersistencePath: storePath})
	if err := engine.Recover(context.Background()); err != nil {
		t.Fatalf("recover jobs: %v", err)
	}

	terminal, found := engine.Get("job_terminal")
	if !found {
		t.Fatal("expected the terminal job to be restored")
	}
	if terminal.Status != StatusSuccess || !terminal.Success || terminal.Message != "job success" {
		t.Fatalf("expected the terminal job to be restored as-is, got %+v", terminal)
	}
	if terminal.StartedAt != 100 || terminal.EndedAt != 200 {
		t.Fatalf("expected the terminal timestamps to be preserved, got started=%d ended=%d", terminal.StartedAt, terminal.EndedAt)
	}

	for _, jobID := range []string{"job_running", "job_queued", "job_ocr"} {
		job, found := engine.Get(jobID)
		if !found {
			t.Fatalf("expected the recovered job %s to be available", jobID)
		}
		if job.Status != StatusInterrupted || job.Success {
			t.Fatalf("expected %s to be interrupted, got %+v", jobID, job)
		}
		if job.Message != "job interrupted after restart" {
			t.Fatalf("expected the interruption message for %s, got %q", jobID, job.Message)
		}
		if job.EndedAt == 0 {
			t.Fatalf("expected an end timestamp for the interrupted job %s", jobID)
		}
		if job.Error == nil || job.Error.DetailCode != "JOB_INTERRUPTED" {
			t.Fatalf("expected the JOB_INTERRUPTED error for %s, got %+v", jobID, job.Error)
		}
		if job.Progress.Stage != StatusInterrupted {
			t.Fatalf("expected interrupted progress stage for %s, got %+v", jobID, job.Progress)
		}
		if job.Progress.Message == "" {
			t.Fatalf("expected a progress message for %s, got %+v", jobID, job.Progress)
		}
		if jobID == "job_ocr" && job.Progress.Message != "interrupted" {
			t.Fatalf("expected the empty ocr progress message to be replaced, got %q", job.Progress.Message)
		}
		if job.Progress.Total > 0 && job.Progress.Current > job.Progress.Total {
			t.Fatalf("expected progress to stay within the total for %s, got %+v", jobID, job.Progress)
		}
	}

	recovered := readEnginePersistedJobs(t, storePath)
	if len(recovered) != len(persisted) {
		t.Fatalf("expected %d persisted jobs after recovery, got %d", len(persisted), len(recovered))
	}
	for _, job := range recovered {
		if !isTerminalJobStatus(job.Status) {
			t.Fatalf("expected only terminal jobs after recovery, got %s (%s)", job.Status, job.JobID)
		}
	}
}

func TestEngineValidateResponseShapes(t *testing.T) {
	invalidErr := models.NewCanonicalJobError("IMAGE_INPUT_MISSING", "input missing", nil)
	validTool := &engineFakeTool{id: "tool.fake.validate.ok"}
	invalidTool := &engineFakeTool{id: "tool.fake.validate.bad", validateErr: invalidErr}
	engine := newTestEngine(t, newEngineResolver(t, validTool, invalidTool), Options{})

	t.Run("valid request", func(t *testing.T) {
		response := engine.Validate(context.Background(), models.JobRequestV1{ToolID: validTool.id})
		if !response.Success || !response.Valid {
			t.Fatalf("response = %+v, want a valid result", response)
		}
		if response.Message != "job request is valid" || response.Error != nil {
			t.Fatalf("response = %+v, want the canonical valid message", response)
		}
	})

	t.Run("invalid request", func(t *testing.T) {
		response := engine.Validate(context.Background(), models.JobRequestV1{ToolID: invalidTool.id})
		if response.Success || response.Valid {
			t.Fatalf("response = %+v, want an invalid result", response)
		}
		if response.Message != "validation failed" {
			t.Fatalf("message = %q, want validation failed", response.Message)
		}
		if response.Error == nil || response.Error.DetailCode != "IMAGE_INPUT_MISSING" {
			t.Fatalf("error = %+v, want IMAGE_INPUT_MISSING", response.Error)
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		response := engine.Validate(context.Background(), models.JobRequestV1{ToolID: "tool.unknown"})
		if response.Success || response.Valid {
			t.Fatalf("response = %+v, want an invalid result", response)
		}
		if response.Message != "tool not found" {
			t.Fatalf("message = %q, want tool not found", response.Message)
		}
		if response.Error == nil || response.Error.DetailCode != "TOOL_NOT_FOUND" {
			t.Fatalf("error = %+v, want TOOL_NOT_FOUND", response.Error)
		}
	})
}

func TestEnginePlanDelegatesToPlannerTool(t *testing.T) {
	planned := models.PlannedOutputV1{
		InputPath:  "/tmp/input.png",
		OutputPath: "/tmp/input.webp",
		OutputKind: "file",
	}
	tool := &fakePlannerTool{
		id: "tool.fake.plan",
		plan: models.PlanJobResponseV1{
			Success: true,
			Message: "ready",
			Outputs: []models.PlannedOutputV1{planned},
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{})
	plan := engine.Plan(context.Background(), models.JobRequestV1{ToolID: tool.id, Mode: "single", InputPaths: []string{planned.InputPath}})

	if !tool.called {
		t.Fatal("expected the planner tool to be called")
	}
	if !plan.Success {
		t.Fatalf("expected successful plan, got %q", plan.Message)
	}
	if len(plan.Outputs) != 1 || plan.Outputs[0].OutputPath != planned.OutputPath {
		t.Fatalf("expected planned output %q, got %+v", planned.OutputPath, plan.Outputs)
	}
	if plan.Error != nil {
		t.Fatalf("expected no plan error, got %+v", plan.Error)
	}
}

func TestEnginePlanReportsUnsupportedTool(t *testing.T) {
	batchTool := &fakeBatchTool{id: "tool.no.plan"}
	engine := newTestEngine(t, newEngineResolver(t, batchTool), Options{})

	plan := engine.Plan(context.Background(), models.JobRequestV1{ToolID: batchTool.id, Mode: "batch"})
	if plan.Success {
		t.Fatal("expected unsupported plan to fail")
	}
	if plan.Error == nil || plan.Error.DetailCode != "PLAN_UNSUPPORTED" {
		t.Fatalf("expected PLAN_UNSUPPORTED, got %+v", plan.Error)
	}
	if plan.Outputs == nil {
		t.Fatal("expected a non-nil outputs slice")
	}
}

func TestEnginePlanReportsUnknownTool(t *testing.T) {
	tool := &fakePlannerTool{id: "tool.known"}
	engine := newTestEngine(t, newEngineResolver(t, tool), Options{})

	plan := engine.Plan(context.Background(), models.JobRequestV1{ToolID: "tool.unknown"})
	if plan.Success {
		t.Fatal("expected unknown tool plan to fail")
	}
	if plan.Error == nil || plan.Error.DetailCode != "TOOL_NOT_FOUND" {
		t.Fatalf("expected TOOL_NOT_FOUND, got %+v", plan.Error)
	}
	if plan.Outputs == nil {
		t.Fatal("expected a non-nil outputs slice")
	}
}

func TestEnginePlanSurfacesPlannerError(t *testing.T) {
	tool := &fakePlannerTool{
		id:      "tool.fake.failing",
		plan:    models.PlanJobResponseV1{Message: "conflict"},
		planErr: models.NewCanonicalJobError("IMAGE_OUTPUT_EXISTS", "output already exists", nil),
	}
	engine := newTestEngine(t, newEngineResolver(t, tool), Options{})

	plan := engine.Plan(context.Background(), models.JobRequestV1{ToolID: tool.id, Mode: "single"})
	if plan.Success {
		t.Fatal("expected failing plan")
	}
	if plan.Error == nil {
		t.Fatal("expected a plan error")
	}
	if plan.Error.DetailCode != "IMAGE_OUTPUT_EXISTS" {
		t.Fatalf("expected detail code IMAGE_OUTPUT_EXISTS, got %q", plan.Error.DetailCode)
	}
}

func TestEnginePlanOnceExecutesStoredPlan(t *testing.T) {
	outputs := []models.PlannedOutputV1{
		{InputPath: engineInput("a.pdf"), OutputPath: engineInput("a-merged.pdf"), OutputKind: "file"},
		{InputPath: engineInput("b.pdf"), OutputPath: engineInput("b-merged.pdf"), OutputKind: "file"},
	}
	resolved := models.PlanJobResponseV1{
		Success: true,
		Message: "ready",
		Outputs: outputs,
		EffectiveOptions: map[string]any{
			"quality":         "high",
			"inputPageRanges": []string{"1", ""},
		},
	}
	tool := &plannedFakeTool{
		id: "tool.fake.plan.once",
		planHook: func(_ context.Context, _ models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
			return resolved, nil
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "batch",
		InputPaths: []string{engineInput("a.pdf"), engineInput("b.pdf")},
	})

	result := awaitEngineJob(t, engine, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success (%s)", result.Status, result.Message)
	}
	if got := tool.planCalls.Load(); got != 1 {
		t.Fatalf("Plan calls = %d, want exactly 1", got)
	}
	if got := tool.execCalls.Load(); got != 1 {
		t.Fatalf("ExecutePlan calls = %d, want exactly 1", got)
	}

	plans := tool.recordedPlans()
	if len(plans) != 1 {
		t.Fatalf("recorded plans = %d, want 1", len(plans))
	}
	received := plans[0]
	if len(received.Outputs) != len(outputs) {
		t.Fatalf("executed plan outputs = %d, want %d", len(received.Outputs), len(outputs))
	}
	for index, output := range outputs {
		if received.Outputs[index].OutputPath != output.OutputPath {
			t.Fatalf("output %d path = %q, want %q", index, received.Outputs[index].OutputPath, output.OutputPath)
		}
	}
	if quality, _ := received.EffectiveOptions["quality"].(string); quality != "high" {
		t.Fatalf("effective quality = %q, want high", quality)
	}
	ranges, ok := received.EffectiveOptions["inputPageRanges"].([]string)
	if !ok || len(ranges) != 2 || ranges[0] != "1" || ranges[1] != "" {
		t.Fatalf("effective inputPageRanges = %#v, want [1 \"\"]", received.EffectiveOptions["inputPageRanges"])
	}
}

func TestEnginePlanFailureRejectsSubmission(t *testing.T) {
	tests := []struct {
		name       string
		plan       models.PlanJobResponseV1
		planErr    *models.JobErrorV1
		wantDetail string
	}{
		{
			name:       "planner error",
			plan:       models.PlanJobResponseV1{Message: "conflict"},
			planErr:    models.NewCanonicalJobError("PDF_MERGE_OUTPUT_ALREADY_EXISTS", "output already exists", nil),
			wantDetail: "PDF_MERGE_OUTPUT_ALREADY_EXISTS",
		},
		{
			name:       "unsuccessful plan without error",
			plan:       models.PlanJobResponseV1{Message: "page ranges invalid"},
			wantDetail: "VALIDATION_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &plannedFakeTool{
				id: "tool.fake.plan.reject",
				planHook: func(_ context.Context, _ models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
					return tt.plan, tt.planErr
				},
			}
			engine := newTestEngine(t, newEngineResolver(t, tool), Options{
				Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
			})

			response, err := engine.Submit(context.Background(), models.JobRequestV1{
				ToolID:     tool.id,
				Mode:       "single",
				InputPaths: []string{engineInput("rejected.pdf")},
			})
			if err != nil {
				t.Fatalf("Submit returned error: %v", err)
			}
			if response.Success || response.Status != StatusFailed || response.Message != "job validation failed" {
				t.Fatalf("response = %+v, want the canonical validation rejection", response)
			}
			if response.Error == nil || response.Error.DetailCode != tt.wantDetail {
				t.Fatalf("response error = %+v, want detail %q", response.Error, tt.wantDetail)
			}
			if got := tool.planCalls.Load(); got != 1 {
				t.Fatalf("Plan calls = %d, want exactly 1", got)
			}
			if got := tool.execCalls.Load(); got != 0 {
				t.Fatalf("ExecutePlan calls = %d, want 0", got)
			}
			if jobs := engine.List(0); len(jobs) != 0 {
				t.Fatalf("rejected job was tracked: %+v", jobs)
			}
		})
	}
}

func TestEnginePlannedBatchRetriesOnlyFailedOutputs(t *testing.T) {
	inputs := []string{engineInput("one.pdf"), engineInput("two.pdf"), engineInput("three.pdf")}
	outputs := make([]models.PlannedOutputV1, 0, len(inputs))
	for _, input := range inputs {
		outputs = append(outputs, models.PlannedOutputV1{InputPath: input, OutputPath: input + ".out", OutputKind: "file"})
	}

	var tool *plannedFakeTool
	tool = &plannedFakeTool{
		id: "tool.image.crop",
		planHook: func(_ context.Context, _ models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
			return models.PlanJobResponseV1{Success: true, Outputs: outputs}, nil
		},
		execHook: func(_ context.Context, _ models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
			call := tool.execCalls.Load()
			items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
			var batchErr *models.JobErrorV1
			for index, output := range plan.Outputs {
				if onProgress != nil {
					onProgress(models.JobProgressV1{Current: index + 1, Total: len(plan.Outputs), Stage: "running"})
				}
				if call == 1 && output.InputPath == inputs[1] {
					jobErr := models.NewCanonicalJobError("IMAGE_CROP_BATCH_ITEM", "transient failure", nil)
					batchErr = jobErr
					items = append(items, models.JobResultItemV1{
						InputPath:  output.InputPath,
						OutputPath: output.OutputPath,
						Success:    false,
						Message:    jobErr.Message,
						Error:      jobErr,
					})
					continue
				}
				items = append(items, models.JobResultItemV1{
					InputPath:   output.InputPath,
					OutputPath:  output.OutputPath,
					Outputs:     []string{output.OutputPath},
					OutputCount: 1,
					Success:     true,
					Message:     "cropped",
				})
			}

			return items, batchErr
		},
	}

	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "batch",
		InputPaths: inputs,
	})

	result := awaitEngineJob(t, engine, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success (%s)", result.Status, result.Message)
	}
	if got := tool.planCalls.Load(); got != 1 {
		t.Fatalf("Plan calls = %d, want exactly 1 across retries", got)
	}

	plans := tool.recordedPlans()
	if len(plans) != 2 {
		t.Fatalf("ExecutePlan calls = %d, want 2", len(plans))
	}
	if len(plans[0].Outputs) != len(outputs) {
		t.Fatalf("first plan outputs = %d, want %d", len(plans[0].Outputs), len(outputs))
	}
	if len(plans[1].Outputs) != 1 || plans[1].Outputs[0].InputPath != inputs[1] {
		t.Fatalf("retry plan outputs = %+v, want only %q", plans[1].Outputs, inputs[1])
	}

	if len(result.Items) != len(inputs) {
		t.Fatalf("items = %d, want %d", len(result.Items), len(inputs))
	}
	wantAttempts := []int{1, 2, 1}
	for index, want := range wantAttempts {
		item := result.Items[index]
		if !item.Success {
			t.Fatalf("item %d failed: %+v", index, item)
		}
		if item.InputPath != inputs[index] {
			t.Fatalf("item %d input = %q, want %q", index, item.InputPath, inputs[index])
		}
		if item.Attempts != want || item.RetryCount != want-1 {
			t.Fatalf("item %d attempts/retryCount = %d/%d, want %d/%d", index, item.Attempts, item.RetryCount, want, want-1)
		}
	}
}

func TestEngineUsesManifestSchedulingMetadata(t *testing.T) {
	plan := models.PlanJobResponseV1{
		Success: true,
		Outputs: []models.PlannedOutputV1{
			{InputPath: engineInput("manifest.pdf"), OutputPath: engineInput("manifest-out.pdf"), OutputKind: "file"},
		},
	}
	newTool := func(id, resourceClass string, maxWorkers int) *plannedFakeTool {
		return &plannedFakeTool{
			id: id,
			manifest: models.ToolManifestV1{
				ToolID:        id,
				ResourceClass: resourceClass,
				MaxWorkers:    maxWorkers,
			},
			planHook: func(_ context.Context, _ models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
				return plan, nil
			},
		}
	}
	request := func(tool *plannedFakeTool) models.JobRequestV1 {
		return models.JobRequestV1{
			ToolID:     tool.id,
			Mode:       "single",
			InputPaths: []string{engineInput("manifest.pdf")},
		}
	}

	t.Run("manifest values apply when submit options are empty", func(t *testing.T) {
		tool := newTool("tool.fake.manifest", "external", 3)
		engine := newTestEngine(t, newEngineResolver(t, tool), Options{
			Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
		})

		response := submitTestJobWith(t, engine, request(tool), SubmitOptions{Kind: "tool", Priority: PriorityInteractive})

		state := engine.lookup(response.JobID)
		if state == nil {
			t.Fatalf("job %s not found", response.JobID)
		}
		if state.class != ClassExternal {
			t.Fatalf("job class = %q, want %q from the manifest", state.class, ClassExternal)
		}
		if state.workers != 3 {
			t.Fatalf("job workers = %d, want 3 from the manifest", state.workers)
		}
	})

	t.Run("explicit submit options override the manifest", func(t *testing.T) {
		tool := newTool("tool.fake.manifest.override", "external", 3)
		engine := newTestEngine(t, newEngineResolver(t, tool), Options{
			Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
		})

		response := submitTestJobWith(t, engine, request(tool), SubmitOptions{
			Kind:     "tool",
			Priority: PriorityInteractive,
			Class:    ClassIO,
			Workers:  1,
		})

		state := engine.lookup(response.JobID)
		if state == nil {
			t.Fatalf("job %s not found", response.JobID)
		}
		if state.class != ClassIO {
			t.Fatalf("job class = %q, want the explicit %q", state.class, ClassIO)
		}
		if state.workers != 1 {
			t.Fatalf("job workers = %d, want the explicit 1", state.workers)
		}
	})
}

func TestEnginePlannerWithoutPlanExecutorUsesLegacyExecution(t *testing.T) {
	output := models.PlannedOutputV1{
		InputPath:  engineInput("legacy.pdf"),
		OutputPath: engineInput("legacy-out.pdf"),
		OutputKind: "file",
	}
	tool := &plannerOnlyFakeTool{
		id: "tool.fake.planner.only",
		plan: models.PlanJobResponseV1{
			Success: true,
			Message: "ready",
			Outputs: []models.PlannedOutputV1{output},
		},
	}
	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits: Limits{Total: 1, CPU: 1, External: 1, IO: 1},
	})

	response := submitTestJob(t, engine, models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{output.InputPath},
	})

	result := awaitEngineJob(t, engine, response.JobID)
	if result.Status != StatusSuccess {
		t.Fatalf("job status = %s, want success (%s)", result.Status, result.Message)
	}
	if got := tool.planCalls.Load(); got != 1 {
		t.Fatalf("Plan calls = %d, want exactly 1 at submit", got)
	}
	if got := tool.calls.Load(); got != 1 {
		t.Fatalf("legacy ExecuteSingle calls = %d, want 1", got)
	}
	state := engine.lookup(response.JobID)
	if state == nil || state.plan == nil {
		t.Fatalf("expected the resolved plan to be stored on the job state")
	}
	if len(result.Items) != 1 || result.Items[0].OutputPath != output.OutputPath {
		t.Fatalf("legacy items = %+v, want the planned output %q", result.Items, output.OutputPath)
	}
}

func seedEngineJob(t *testing.T, engine *Engine, result models.JobResultV1) {
	t.Helper()

	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.jobs[result.JobID] = &jobState{
		engine:   engine,
		result:   result,
		terminal: isTerminalJobStatus(result.Status),
		done:     make(chan struct{}),
	}
}

func engineTrackedJobIDs(engine *Engine) map[string]struct{} {
	engine.mu.RLock()
	defer engine.mu.RUnlock()

	ids := make(map[string]struct{}, len(engine.jobs))
	for jobID := range engine.jobs {
		ids[jobID] = struct{}{}
	}

	return ids
}

func assertEngineJobOrder(t *testing.T, jobs []models.JobResultV1, want []string) {
	t.Helper()

	if len(jobs) != len(want) {
		t.Fatalf("expected %v, got %d jobs", want, len(jobs))
	}
	for index := range want {
		if jobs[index].JobID != want[index] {
			got := make([]string, len(jobs))
			for jobIndex, job := range jobs {
				got[jobIndex] = job.JobID
			}
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func engineJobIDForIndex(index int) string {
	return fmt.Sprintf("job_terminal_%02d", index)
}

func awaitJobCompletions(t *testing.T, completions <-chan models.JobResultV1, want int) []models.JobResultV1 {
	t.Helper()

	results := make([]models.JobResultV1, 0, want)
	deadline := time.After(5 * time.Second)
	for len(results) < want {
		select {
		case result := <-completions:
			results = append(results, result)
		case <-deadline:
			t.Fatalf("timed out waiting for %d completion callbacks, got %d", want, len(results))
		}
	}

	return results
}

func assertNoExtraCompletion(t *testing.T, completions <-chan models.JobResultV1, window time.Duration) {
	t.Helper()

	select {
	case extra := <-completions:
		t.Fatalf("unexpected extra completion callback: %+v", extra)
	case <-time.After(window):
	}
}
