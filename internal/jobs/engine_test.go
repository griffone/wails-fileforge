package jobs

import (
	"context"
	"encoding/json"
	"errors"
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
