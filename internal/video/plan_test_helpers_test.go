package video

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
)

type stubRuntimeProbe struct {
	err error
}

func (p stubRuntimeProbe) Check(context.Context) error {
	return p.err
}

// stubCommandRunner records ffmpeg invocations without executing them.
type stubCommandRunner struct {
	calls [][]string
}

func (r *stubCommandRunner) Run(_ context.Context, name string, args []string) error {
	combined := append([]string{name}, args...)
	r.calls = append(r.calls, combined)
	return nil
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

func videoToolRequest(toolID, mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
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
