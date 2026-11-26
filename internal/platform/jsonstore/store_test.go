package jsonstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"fileforge-desktop/internal/platform/fsx"
)

func TestLoad(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"name":"forge","count":3}`), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var got payload
	if err := Load(path, &got); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != "forge" || got.Count != 3 {
		t.Fatalf("loaded payload = %+v, want {forge 3}", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	err := Load(filepath.Join(t.TempDir(), "missing.json"), &struct{}{})
	if err == nil {
		t.Fatal("Load on a missing file returned nil error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want os.ErrNotExist", err)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if err := Load(path, &struct{}{}); err == nil {
		t.Fatal("Load on invalid JSON returned nil error")
	}
}

func TestSaveNowWritesImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: time.Hour})

	payload := []byte(`{"mode":"now"}`)
	if err := store.SaveNow(payload); err != nil {
		t.Fatalf("SaveNow: %v", err)
	}
	assertFileContent(t, path, payload)
	assertNoStoreTempFiles(t, filepath.Dir(path))
}

func TestSaveDefersTheWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	if err := store.Save([]byte(`{"mode":"debounced"}`)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if fsx.Exists(path) {
		t.Fatal("Save wrote the file before the debounce interval elapsed")
	}
}

func TestSaveCoalescesRapidWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 200 * time.Millisecond})

	first := []byte(`{"value":1}`)
	second := []byte(`{"value":2}`)

	stop := make(chan struct{})
	watcherDone := make(chan struct{})
	var observed []string
	go func() {
		defer close(watcherDone)
		var last string
		for {
			select {
			case <-stop:
				return
			default:
			}
			if data, err := os.ReadFile(path); err == nil && string(data) != last {
				last = string(data)
				observed = append(observed, last)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	if err := store.Save(first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := store.Save(second); err != nil {
		t.Fatalf("Save second: %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	close(stop)
	<-watcherDone

	assertFileContent(t, path, second)
	for _, content := range observed {
		if content == string(first) {
			t.Fatalf("coalesced save wrote the superseded payload %q", content)
		}
	}
}

func TestFlushWritesPendingPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	payload := []byte(`{"flushed":true}`)
	if err := store.Save(payload); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	assertFileContent(t, path, payload)
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
}

func TestFlushHonorsCancelledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	payload := []byte(`{"flushed":true}`)
	if err := store.Save(payload); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Flush error = %v, want context.Canceled", err)
	}
	if fsx.Exists(path) {
		t.Fatal("Flush wrote the file despite a cancelled context")
	}

	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush after cancellation: %v", err)
	}
	assertFileContent(t, path, payload)
}

func TestSaveNowSupersedesPendingPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 50 * time.Millisecond})

	oldPayload := []byte(`{"value":"old"}`)
	newPayload := []byte(`{"value":"new"}`)

	if err := store.Save(oldPayload); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.SaveNow(newPayload); err != nil {
		t.Fatalf("SaveNow: %v", err)
	}
	assertFileContent(t, path, newPayload)

	time.Sleep(150 * time.Millisecond)
	assertFileContent(t, path, newPayload)
}

func TestCloseFlushesPendingPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := New(path, Options{Debounce: 5 * time.Second})

	payload := []byte(`{"closed":true}`)
	if err := store.Save(payload); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertFileContent(t, path, payload)

	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := store.Save([]byte(`{}`)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Save after Close error = %v, want ErrClosed", err)
	}
	if err := store.SaveNow([]byte(`{}`)); !errors.Is(err, ErrClosed) {
		t.Fatalf("SaveNow after Close error = %v, want ErrClosed", err)
	}
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush after Close: %v", err)
	}
}

func TestCloseHonorsCancelledContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	if err := store.Save([]byte(`{"pending":true}`)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close error = %v, want context.Canceled", err)
	}

	// A failed Close must leave the store usable.
	if err := store.SaveNow([]byte(`{"retried":true}`)); err != nil {
		t.Fatalf("SaveNow after failed Close: %v", err)
	}
	assertFileContent(t, path, []byte(`{"retried":true}`))
}

func TestOnErrorReportsWriteFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	errorsCh := make(chan error, 1)
	store := New(filepath.Join(blocker, "state.json"), Options{
		Debounce: 20 * time.Millisecond,
		OnError: func(err error) {
			select {
			case errorsCh <- err:
			default:
			}
		},
	})
	t.Cleanup(func() { _ = store.Close(context.Background()) })

	if err := store.Save([]byte(`{}`)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	select {
	case err := <-errorsCh:
		if err == nil {
			t.Fatal("OnError received a nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnError was not called for a failed debounced write")
	}
}

func TestSaveNowReturnsWriteError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	store := New(filepath.Join(blocker, "state.json"), Options{})
	if err := store.SaveNow([]byte(`{}`)); err == nil {
		t.Fatal("SaveNow returned nil error for an unwritable destination")
	}
}

func TestSaveRejectsEmptyPath(t *testing.T) {
	store := New("  ", Options{})
	if err := store.Save([]byte(`{}`)); err == nil {
		t.Fatal("Save with an empty path returned nil error")
	}
	if err := store.SaveNow([]byte(`{}`)); err == nil {
		t.Fatal("SaveNow with an empty path returned nil error")
	}
}

func TestConcurrentSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := New(path, Options{Debounce: 10 * time.Millisecond})
	t.Cleanup(func() { _ = store.Close(context.Background()) })

	payloads := make([][]byte, 0, 64)
	for writer := 0; writer < 8; writer++ {
		for sequence := 0; sequence < 8; sequence++ {
			payloads = append(payloads, []byte(`{"writer":`+strconv.Itoa(writer)+`,"sequence":`+strconv.Itoa(sequence)+`}`))
		}
	}

	var waitGroup sync.WaitGroup
	for _, payload := range payloads {
		payload := payload
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := store.Save(payload); err != nil {
				t.Errorf("Save: %v", err)
			}
		}()
	}
	waitGroup.Wait()

	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final state: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("final state is not valid JSON: %q", data)
	}
	known := false
	for _, payload := range payloads {
		if string(payload) == string(data) {
			known = true
			break
		}
	}
	if !known {
		t.Fatalf("final state %q is not one of the written payloads", data)
	}
}

func TestStoreAppliesFileModeAndCreatesDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", "state.json")
	store := newTestStore(t, path, Options{FileMode: 0o600})

	if err := store.SaveNow([]byte(`{}`)); err != nil {
		t.Fatalf("SaveNow: %v", err)
	}
	if !fsx.Exists(path) {
		t.Fatalf("%s was not created", path)
	}
	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
	if !fsx.IsDir(filepath.Dir(filepath.Dir(path))) {
		t.Fatal("nested directories were not created")
	}
}

func TestCustomTempPatternIsUsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{TempPattern: "custom-*.tmp"})

	if err := store.SaveNow([]byte(`{}`)); err != nil {
		t.Fatalf("SaveNow: %v", err)
	}
	assertNoStoreTempFiles(t, filepath.Dir(path))
}

func TestInvalidTempPatternReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{TempPattern: "bad/pattern-*.tmp"})

	err := store.SaveNow([]byte(`{}`))
	if err == nil {
		t.Fatal("SaveNow returned nil error for a temp pattern with a path separator")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("error = %v, want a pattern validation error", err)
	}
}

func TestDefaultOptions(t *testing.T) {
	store := New("/tmp/state.json", Options{})
	if store.opts.Debounce != DefaultDebounce {
		t.Fatalf("debounce = %s, want %s", store.opts.Debounce, DefaultDebounce)
	}
	if store.opts.FileMode != defaultFileMode {
		t.Fatalf("file mode = %o, want %o", store.opts.FileMode, defaultFileMode)
	}
	if store.opts.DirMode != defaultDirMode {
		t.Fatalf("dir mode = %o, want %o", store.opts.DirMode, defaultDirMode)
	}
	if !strings.Contains(store.opts.TempPattern, "state.json") {
		t.Fatalf("temp pattern = %q, want it derived from the destination name", store.opts.TempPattern)
	}
}

func newTestStore(t *testing.T, path string, opts Options) *Store {
	t.Helper()

	store := New(path, opts)
	t.Cleanup(func() { _ = store.Close(context.Background()) })

	return store
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != string(want) {
		t.Fatalf("file content = %q, want %q", data, want)
	}
}

// waitForFile waits until the debounced write produced path.
func waitForFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fsx.Exists(path) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("%s was not written before the deadline", path)
}

func assertNoStoreTempFiles(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read directory %s: %v", dir, err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestSaveFuncDefersTheProducer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	calls := 0
	if err := store.SaveFunc(func() ([]byte, error) {
		calls++

		return []byte(`{"lazy":true}`), nil
	}); err != nil {
		t.Fatalf("SaveFunc: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if calls != 0 {
		t.Fatalf("producer ran %d times before the debounce interval elapsed", calls)
	}
	if fsx.Exists(path) {
		t.Fatal("SaveFunc wrote the file before the debounce interval elapsed")
	}
}

func TestSaveFuncRunsTheProducerOnWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 10 * time.Millisecond})

	calls := 0
	if err := store.SaveFunc(func() ([]byte, error) {
		calls++

		return []byte(`{"lazy":true}`), nil
	}); err != nil {
		t.Fatalf("SaveFunc: %v", err)
	}

	waitForFile(t, path)
	assertFileContent(t, path, []byte(`{"lazy":true}`))
	if calls != 1 {
		t.Fatalf("producer ran %d times, want 1", calls)
	}
}

func TestSaveFuncCoalescesToTheLastProducer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 150 * time.Millisecond})

	firstCalls := 0
	secondCalls := 0
	if err := store.SaveFunc(func() ([]byte, error) {
		firstCalls++

		return []byte(`{"value":1}`), nil
	}); err != nil {
		t.Fatalf("SaveFunc first: %v", err)
	}
	if err := store.SaveFunc(func() ([]byte, error) {
		secondCalls++

		return []byte(`{"value":2}`), nil
	}); err != nil {
		t.Fatalf("SaveFunc second: %v", err)
	}

	waitForFile(t, path)
	assertFileContent(t, path, []byte(`{"value":2}`))
	if firstCalls != 0 {
		t.Fatalf("superseded producer ran %d times", firstCalls)
	}
	if secondCalls != 1 {
		t.Fatalf("last producer ran %d times, want 1", secondCalls)
	}
}

func TestSaveFuncKeepsTheLatestProducerAcrossMixedSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	if err := store.SaveFunc(func() ([]byte, error) {
		t.Fatal("superseded producer ran")

		return nil, nil
	}); err != nil {
		t.Fatalf("SaveFunc: %v", err)
	}
	if err := store.Save([]byte(`{"eager":true}`)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	assertFileContent(t, path, []byte(`{"eager":true}`))
}

func TestFlushRunsThePendingProducer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	if err := store.SaveFunc(func() ([]byte, error) {
		return []byte(`{"flushed":true}`), nil
	}); err != nil {
		t.Fatalf("SaveFunc: %v", err)
	}
	if err := store.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	assertFileContent(t, path, []byte(`{"flushed":true}`))
}

func TestCloseRunsThePendingProducer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	if err := store.SaveFunc(func() ([]byte, error) {
		return []byte(`{"closed":true}`), nil
	}); err != nil {
		t.Fatalf("SaveFunc: %v", err)
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	assertFileContent(t, path, []byte(`{"closed":true}`))
}

func TestSaveNowDiscardsThePendingProducer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := newTestStore(t, path, Options{Debounce: 5 * time.Second})

	if err := store.SaveFunc(func() ([]byte, error) {
		t.Fatal("superseded producer ran")

		return nil, nil
	}); err != nil {
		t.Fatalf("SaveFunc: %v", err)
	}
	if err := store.SaveNow([]byte(`{"immediate":true}`)); err != nil {
		t.Fatalf("SaveNow: %v", err)
	}

	assertFileContent(t, path, []byte(`{"immediate":true}`))
}

func TestOnErrorReportsProducerFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	errorsCh := make(chan error, 1)
	store := New(path, Options{
		Debounce: 10 * time.Millisecond,
		OnError: func(err error) {
			select {
			case errorsCh <- err:
			default:
			}
		},
	})
	t.Cleanup(func() { _ = store.Close(context.Background()) })

	producerErr := errors.New("encode failed")
	if err := store.SaveFunc(func() ([]byte, error) {
		return nil, producerErr
	}); err != nil {
		t.Fatalf("SaveFunc: %v", err)
	}

	select {
	case err := <-errorsCh:
		if !errors.Is(err, producerErr) {
			t.Fatalf("OnError error = %v, want %v", err, producerErr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnError was not called for a failed producer")
	}
	if fsx.Exists(path) {
		t.Fatal("a failed producer still wrote the file")
	}
}

func TestSaveFuncRejectsEmptyPath(t *testing.T) {
	store := New("  ", Options{})
	if err := store.SaveFunc(func() ([]byte, error) { return nil, nil }); err == nil {
		t.Fatal("SaveFunc with an empty path returned nil error")
	}
}

func TestSaveFuncAfterCloseReturnsErrClosed(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "state.json"), Options{})
	if err := store.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := store.SaveFunc(func() ([]byte, error) { return nil, nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("SaveFunc after Close = %v, want ErrClosed", err)
	}
}
