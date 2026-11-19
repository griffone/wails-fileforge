package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"fileforge-desktop/internal/testutil"
)

// updateGolden refreshes the golden files instead of comparing them.
var updateGolden = flag.Bool("update", false, "update golden files")

// newTestApp builds an App with isolated user directories and shuts it down
// when the test finishes.
func newTestApp(t *testing.T) *App {
	t.Helper()

	t.Setenv("XDG_CONFIG_HOME", testutil.TempDir(t))
	t.Setenv("XDG_DATA_HOME", testutil.TempDir(t))
	t.Setenv("XDG_CACHE_HOME", testutil.TempDir(t))

	a := New(NewNotifier())
	t.Cleanup(func() {
		if err := a.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown app: %v", err)
		}
	})

	return a
}

func TestListToolsContract(t *testing.T) {
	a := newTestApp(t)

	response := a.ListToolsV1()
	if !response.Success {
		t.Fatalf("ListToolsV1 failed: %s", response.Message)
	}
	if len(response.Tools) != 20 {
		t.Fatalf("listed tools = %d, want 20", len(response.Tools))
	}

	ids := make([]string, 0, len(response.Tools))
	for _, entry := range response.Tools {
		if entry.Manifest.ToolID == "" {
			t.Fatalf("tool with empty ID in ListToolsV1: %+v", entry)
		}
		ids = append(ids, entry.Manifest.ToolID)
	}
	sort.Strings(ids)

	assertGoldenJSON(t, filepath.Join("testdata", "tool_ids.golden.json"), ids)
}

func TestListRecipesContract(t *testing.T) {
	a := newTestApp(t)

	response := a.ListRecipesV1()
	if !response.Success {
		t.Fatalf("ListRecipesV1 failed: %s", response.Message)
	}
	if len(response.Recipes) != 5 {
		t.Fatalf("listed recipes = %d, want 5", len(response.Recipes))
	}

	// The built-in order is part of the contract (Email PDF first).
	ids := make([]string, 0, len(response.Recipes))
	for _, recipe := range response.Recipes {
		if recipe.ID == "" {
			t.Fatalf("recipe with empty ID in ListRecipesV1: %+v", recipe)
		}
		ids = append(ids, recipe.ID)
	}

	assertGoldenJSON(t, filepath.Join("testdata", "recipe_ids.golden.json"), ids)
}

func TestGetLocalCapabilitiesContract(t *testing.T) {
	a := newTestApp(t)

	response := a.GetLocalCapabilitiesV1()
	if !response.Success {
		t.Fatalf("GetLocalCapabilitiesV1 failed: %s", response.Message)
	}
	if len(response.Checks) == 0 {
		t.Fatal("GetLocalCapabilitiesV1 returned no checks")
	}
}

func TestGetToolchainStatusContract(t *testing.T) {
	a := newTestApp(t)

	status := a.GetToolchainStatusV1()
	if status.Message == "" {
		t.Fatal("GetToolchainStatusV1 returned an empty message")
	}
}

// assertGoldenJSON compares value, encoded as indented JSON, against the golden
// file. Run the package tests with -update to refresh the golden file.
func assertGoldenJSON(t *testing.T, path string, value any) {
	t.Helper()

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden value: %v", err)
	}
	payload = append(payload, '\n')

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, payload, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file: %v (run `go test ./internal/app -update` to create it)", err)
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("contract changed; run `go test ./internal/app -update` to refresh %s", path)
	}
}
