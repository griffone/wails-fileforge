package toolchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeEnvironment struct {
	values map[string]string
}

func (e *fakeEnvironment) getenv(key string) string {
	return e.values[key]
}

func (e *fakeEnvironment) setenv(key, value string) error {
	e.values[key] = value

	return nil
}

func TestApplyManagedEnvironmentUnix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "toolchain")
	if err := os.MkdirAll(tessdataDir(root), 0o755); err != nil {
		t.Fatalf("create tessdata: %v", err)
	}
	environment := &fakeEnvironment{values: map[string]string{
		"PATH": "/usr/local/bin:/usr/bin:/bin",
	}}

	if err := applyManagedEnvironment(root, "linux", environment.getenv, environment.setenv); err != nil {
		t.Fatalf("apply environment: %v", err)
	}

	pathEntries := strings.Split(environment.values["PATH"], string(filepath.ListSeparator))
	wantPrefix := []string{
		filepath.Join(root, "env", "bin"),
		filepath.Join(root, "whisper"),
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
	}
	if len(pathEntries) != len(wantPrefix) {
		t.Fatalf("PATH entries = %v, want %v", pathEntries, wantPrefix)
	}
	for index, want := range wantPrefix {
		if pathEntries[index] != want {
			t.Fatalf("PATH entry %d = %q, want %q", index, pathEntries[index], want)
		}
	}
	if got := environment.values["TESSDATA_PREFIX"]; got != filepath.Join(root, "tessdata") {
		t.Fatalf("TESSDATA_PREFIX = %q", got)
	}
}

func TestApplyManagedEnvironmentWindowsLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "toolchain")
	if err := os.MkdirAll(tessdataDir(root), 0o755); err != nil {
		t.Fatalf("create tessdata: %v", err)
	}
	environment := &fakeEnvironment{values: map[string]string{
		"PATH": `C:\Windows\System32`,
	}}

	if err := applyManagedEnvironment(root, "windows", environment.getenv, environment.setenv); err != nil {
		t.Fatalf("apply environment: %v", err)
	}

	updated := environment.values["PATH"]
	if !strings.Contains(updated, ";") {
		t.Fatalf("expected the Windows separator, got %q", updated)
	}

	wantPrefix := []string{
		filepath.Join(root, "env", "Library", "bin"),
		filepath.Join(root, "env", "Scripts"),
		filepath.Join(root, "env", "bin"),
		filepath.Join(root, "whisper"),
		`C:\Windows\System32`,
	}
	pathEntries := strings.Split(updated, ";")
	if len(pathEntries) != len(wantPrefix) {
		t.Fatalf("PATH entries = %v, want %v", pathEntries, wantPrefix)
	}
	for index, want := range wantPrefix {
		if pathEntries[index] != want {
			t.Fatalf("PATH entry %d = %q, want %q", index, pathEntries[index], want)
		}
	}
}

func TestApplyManagedEnvironmentIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "toolchain")
	if err := os.MkdirAll(tessdataDir(root), 0o755); err != nil {
		t.Fatalf("create tessdata: %v", err)
	}
	environment := &fakeEnvironment{values: map[string]string{
		"PATH": "/usr/local/bin:/usr/bin:/bin",
	}}

	if err := applyManagedEnvironment(root, "linux", environment.getenv, environment.setenv); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	firstPath := environment.values["PATH"]

	if err := applyManagedEnvironment(root, "linux", environment.getenv, environment.setenv); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if environment.values["PATH"] != firstPath {
		t.Fatalf("PATH changed on a repeated apply:\n first: %q\nsecond: %q", firstPath, environment.values["PATH"])
	}

	entries := strings.Split(environment.values["PATH"], string(filepath.ListSeparator))
	managedBin := filepath.Join(root, "env", "bin")
	count := 0
	for _, entry := range entries {
		if entry == managedBin {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected the managed bin folder once, got %d in %v", count, entries)
	}
}

func TestApplyManagedEnvironmentRemovesStaleManagedEntries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "toolchain")
	managedBin := filepath.Join(root, "env", "bin")
	environment := &fakeEnvironment{values: map[string]string{
		"PATH": managedBin + string(filepath.ListSeparator) + "/usr/bin",
	}}

	if err := applyManagedEnvironment(root, "linux", environment.getenv, environment.setenv); err != nil {
		t.Fatalf("apply environment: %v", err)
	}

	entries := strings.Split(environment.values["PATH"], string(filepath.ListSeparator))
	count := 0
	for _, entry := range entries {
		if entry == managedBin {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one managed bin entry, got %d in %v", count, entries)
	}
	if entries[0] != managedBin {
		t.Fatalf("expected the managed bin first, got %v", entries)
	}
}

func TestApplyManagedEnvironmentRejectsEmptyRoot(t *testing.T) {
	if err := applyManagedEnvironment("", "linux", nil, nil); err == nil {
		t.Fatal("expected an error for an empty root")
	}
}

func TestApplyManagedEnvironmentKeepsSystemTessdataBeforeInstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "toolchain")
	environment := &fakeEnvironment{values: map[string]string{
		"PATH":            "/usr/bin",
		"TESSDATA_PREFIX": "/usr/share/tessdata",
	}}

	if err := applyManagedEnvironment(root, "linux", environment.getenv, environment.setenv); err != nil {
		t.Fatalf("apply environment: %v", err)
	}

	if got := environment.values["TESSDATA_PREFIX"]; got != "/usr/share/tessdata" {
		t.Fatalf("expected the system TESSDATA_PREFIX to be untouched, got %q", got)
	}
}

func TestApplyManagedEnvironmentSetsTessdataWhenInstalled(t *testing.T) {
	root := filepath.Join(t.TempDir(), "toolchain")
	if err := os.MkdirAll(tessdataDir(root), 0o755); err != nil {
		t.Fatalf("create tessdata: %v", err)
	}
	environment := &fakeEnvironment{values: map[string]string{"PATH": "/usr/bin"}}

	if err := applyManagedEnvironment(root, "linux", environment.getenv, environment.setenv); err != nil {
		t.Fatalf("apply environment: %v", err)
	}

	if got := environment.values["TESSDATA_PREFIX"]; got != tessdataDir(root) {
		t.Fatalf("TESSDATA_PREFIX = %q, want %q", got, tessdataDir(root))
	}
}

func TestPrependPathKeepsUnrelatedEntries(t *testing.T) {
	got := prependPath("/usr/bin:/custom/tool:/bin", []string{"/managed/bin"}, ":")
	if got != "/managed/bin:/usr/bin:/custom/tool:/bin" {
		t.Fatalf("unexpected PATH: %q", got)
	}
}
