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

// newTestApp builds the service bundle with isolated user directories and
// shuts it down when the test finishes.
func newTestApp(t *testing.T) *Services {
	t.Helper()

	t.Setenv("XDG_CONFIG_HOME", testutil.TempDir(t))
	t.Setenv("XDG_DATA_HOME", testutil.TempDir(t))
	t.Setenv("XDG_CACHE_HOME", testutil.TempDir(t))

	bundle := New(NewNotifier())
	t.Cleanup(func() {
		if err := bundle.Lifecycle.shutdown(context.Background()); err != nil {
			t.Errorf("shutdown app: %v", err)
		}
	})

	return bundle
}

func TestListToolsContract(t *testing.T) {
	a := newTestApp(t)

	response := a.Tools.ListTools()
	if !response.Success {
		t.Fatalf("ListTools failed: %s", response.Message)
	}
	if len(response.Tools) != 29 {
		t.Fatalf("listed tools = %d, want 29", len(response.Tools))
	}

	ids := make([]string, 0, len(response.Tools))
	for _, entry := range response.Tools {
		if entry.Manifest.ToolID == "" {
			t.Fatalf("tool with empty ID in ListTools: %+v", entry)
		}
		ids = append(ids, entry.Manifest.ToolID)
	}
	sort.Strings(ids)

	assertGoldenJSON(t, filepath.Join("testdata", "tool_ids.golden.json"), ids)
}

func TestListRecipesContract(t *testing.T) {
	a := newTestApp(t)

	response := a.Recipes.ListRecipes()
	if !response.Success {
		t.Fatalf("ListRecipes failed: %s", response.Message)
	}
	if len(response.Recipes) != 6 {
		t.Fatalf("listed recipes = %d, want 6", len(response.Recipes))
	}

	// The built-in order is part of the contract (Email PDF first).
	ids := make([]string, 0, len(response.Recipes))
	for _, recipe := range response.Recipes {
		if recipe.ID == "" {
			t.Fatalf("recipe with empty ID in ListRecipes: %+v", recipe)
		}
		ids = append(ids, recipe.ID)
	}

	assertGoldenJSON(t, filepath.Join("testdata", "recipe_ids.golden.json"), ids)
}

func TestGetLocalCapabilitiesContract(t *testing.T) {
	a := newTestApp(t)

	response := a.System.GetLocalCapabilities()
	if !response.Success {
		t.Fatalf("GetLocalCapabilities failed: %s", response.Message)
	}
	if len(response.Checks) == 0 {
		t.Fatal("GetLocalCapabilities returned no checks")
	}
}

func TestGetAppInfoContract(t *testing.T) {
	a := newTestApp(t)

	response := a.System.GetAppInfo()
	if !response.Success {
		t.Fatalf("GetAppInfo failed: %s", response.Message)
	}
	if response.Version == "" || response.Commit == "" || response.BuiltAt == "" {
		t.Fatalf("GetAppInfo returned an incomplete identity: %+v", response)
	}
}

func TestGetToolchainStatusContract(t *testing.T) {
	a := newTestApp(t)

	status := a.Toolchain.GetToolchainStatus()
	if status.Message == "" {
		t.Fatal("GetToolchainStatus returned an empty message")
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
