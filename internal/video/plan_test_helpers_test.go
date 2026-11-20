package video

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

type stubRuntimeProbe struct {
	err error
}

func (p stubRuntimeProbe) Check(context.Context) error {
	return p.err
}

// stubCommandRunner records ffmpeg invocations without executing them. Unless a
// run hook is supplied, it emulates a successful render by writing placeholder
// bytes to the output path (the last argument), so atomic publication has
// something to publish.
type stubCommandRunner struct {
	calls [][]string
	run   func(name string, args []string) error
}

func (r *stubCommandRunner) Run(_ context.Context, name string, args []string) error {
	combined := append([]string{name}, args...)
	r.calls = append(r.calls, combined)
	if r.run != nil {
		return r.run(name, args)
	}

	return writeStubCommandOutput(args)
}

func writeStubCommandOutput(args []string) error {
	if len(args) == 0 {
		return nil
	}

	return os.WriteFile(args[len(args)-1], []byte("fixture"), 0o644)
}

// assertAtomicallyPublished verifies the fake ffmpeg wrote a sibling temp file
// with the final extension and the engine published it to wantPath.
func assertAtomicallyPublished(t *testing.T, call []string, wantPath string) {
	t.Helper()

	if len(call) == 0 {
		t.Fatal("expected a recorded ffmpeg call")
	}
	tempPath := call[len(call)-1]
	if tempPath == wantPath {
		t.Fatalf("ffmpeg must write to a temp file, not the final path %q", wantPath)
	}
	if filepath.Dir(tempPath) != filepath.Dir(wantPath) {
		t.Fatalf("temp output %q must live next to %q", tempPath, wantPath)
	}
	if filepath.Ext(tempPath) != filepath.Ext(wantPath) {
		t.Fatalf("temp output %q must keep the %q extension", tempPath, filepath.Ext(wantPath))
	}
	if !strings.HasPrefix(filepath.Base(tempPath), ".fileforge-") {
		t.Fatalf("unexpected temp output name %q", filepath.Base(tempPath))
	}
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Fatalf("expected the published output %q: %v", wantPath, statErr)
	}
}

// writeVideoFixture creates a placeholder file. The video tools only stat and
// probe paths during planning, so the bytes never need to be a real video.
func writeVideoFixture(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func videoToolRequest(toolID, mode string, inputPaths []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     toolID,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

// hasArgSequence reports whether args contains want as a contiguous sequence.
func hasArgSequence(args []string, want ...string) bool {
	if len(want) == 0 {
		return true
	}

	for index := 0; index+len(want) <= len(args); index++ {
		matched := true
		for offset, value := range want {
			if args[index+offset] != value {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

// stubVideoDuration simulates the source duration used by trim planning so
// tests never depend on ffprobe or on real video files.
func stubVideoDuration(t *testing.T, duration float64, ok bool) {
	t.Helper()

	original := probeVideoDuration
	t.Cleanup(func() {
		probeVideoDuration = original
	})

	probeVideoDuration = func(context.Context, string) (float64, bool) {
		return duration, ok
	}
}
