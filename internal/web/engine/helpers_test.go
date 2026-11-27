package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/platform/execx"
)

type runnerCall struct {
	path string
	args []string
}

// fakeRunner scripts yt-dlp without a process. Each call records its arguments
// and can emit stdout lines before returning the canned result.
type fakeRunner struct {
	calls []runnerCall
	run   func(call runnerCall, emit func(line string)) (execx.Result, error)
}

func (r *fakeRunner) Run(_ context.Context, path string, args []string, stdout func(line string)) (execx.Result, error) {
	call := runnerCall{path: path, args: append([]string(nil), args...)}
	r.calls = append(r.calls, call)

	if r.run == nil {
		return execx.Result{}, nil
	}

	return r.run(call, stdout)
}

func loadFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return string(data)
}

func argsContain(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}

	return false
}
