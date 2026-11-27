package toolchain

import (
	"errors"
	"testing"
)

func fakeGhostscriptLookPath(available ...string) LookPathFunc {
	found := make(map[string]string, len(available))
	for _, name := range available {
		found[name] = "/fake/" + name
	}

	return func(name string) (string, error) {
		if path, ok := found[name]; ok {
			return path, nil
		}

		return "", errors.New("executable file not found in $PATH")
	}
}

func TestResolveGhostscript(t *testing.T) {
	tests := []struct {
		name         string
		available    []string
		wantName     string
		wantPath     string
		wantResolved bool
	}{
		{
			name:         "only gs",
			available:    []string{"gs"},
			wantName:     "gs",
			wantPath:     "/fake/gs",
			wantResolved: true,
		},
		{
			name:         "only gswin64c",
			available:    []string{"gswin64c"},
			wantName:     "gswin64c",
			wantPath:     "/fake/gswin64c",
			wantResolved: true,
		},
		{
			name:         "only gswin32c",
			available:    []string{"gswin32c"},
			wantName:     "gswin32c",
			wantPath:     "/fake/gswin32c",
			wantResolved: true,
		},
		{
			name:         "none",
			wantResolved: false,
		},
		{
			name:         "gs wins over both Windows names",
			available:    []string{"gswin32c", "gswin64c", "gs"},
			wantName:     "gs",
			wantPath:     "/fake/gs",
			wantResolved: true,
		},
		{
			name:         "gswin64c wins over gswin32c",
			available:    []string{"gswin32c", "gswin64c"},
			wantName:     "gswin64c",
			wantPath:     "/fake/gswin64c",
			wantResolved: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binary, found := ResolveGhostscript(fakeGhostscriptLookPath(test.available...))
			if found != test.wantResolved {
				t.Fatalf("ResolveGhostscript() found = %t, want %t", found, test.wantResolved)
			}
			if binary.Name != test.wantName {
				t.Fatalf("ResolveGhostscript() name = %q, want %q", binary.Name, test.wantName)
			}
			if binary.Path != test.wantPath {
				t.Fatalf("ResolveGhostscript() path = %q, want %q", binary.Path, test.wantPath)
			}
		})
	}
}

func TestResolveGhostscriptIgnoresBlankPath(t *testing.T) {
	binary, found := ResolveGhostscript(func(name string) (string, error) {
		return "   ", nil
	})
	if found {
		t.Fatalf("expected a blank path to be rejected, got %+v", binary)
	}
}

func TestGhostscriptBinaryNamesOrder(t *testing.T) {
	want := []string{"gs", "gswin64c", "gswin32c"}
	if len(GhostscriptBinaryNames) != len(want) {
		t.Fatalf("unexpected names: %v", GhostscriptBinaryNames)
	}
	for index, name := range want {
		if GhostscriptBinaryNames[index] != name {
			t.Fatalf("name %d = %q, want %q", index, GhostscriptBinaryNames[index], name)
		}
	}
}
