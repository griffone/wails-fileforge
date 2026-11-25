package registry

import (
	"context"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

// fakeTool is a minimal tools.Tool used to exercise registration isolation.
type fakeTool struct {
	id string
}

func (t fakeTool) ID() string         { return t.id }
func (t fakeTool) Capability() string { return t.id }
func (t fakeTool) Manifest() models.ToolManifest {
	return models.ToolManifest{ToolID: t.id}
}
func (t fakeTool) RuntimeState(context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{}
}
func (t fakeTool) Validate(context.Context, models.JobRequest) *models.JobError {
	return nil
}

func TestGetToolResolvesOtherToolsAfterDuplicateRegistration(t *testing.T) {
	reg := NewRegistry()
	reg.SafeRegisterTool(fakeTool{id: "tool.alpha"})
	reg.SafeRegisterTool(fakeTool{id: "tool.beta"})
	reg.SafeRegisterTool(fakeTool{id: "tool.alpha"})

	for _, id := range []string{"tool.alpha", "tool.beta"} {
		tool, err := reg.GetTool(id)
		if err != nil {
			t.Fatalf("GetTool(%q) failed after a duplicate registration: %v", id, err)
		}
		if tool.ID() != id {
			t.Fatalf("GetTool(%q) resolved %q", id, tool.ID())
		}
	}

	errs := reg.GetInitializationErrors()
	if len(errs) != 1 {
		t.Fatalf("expected exactly one registration error, got %v", errs)
	}
	if message := errs[0].Error(); !strings.Contains(message, "tool.alpha") || !strings.Contains(message, "already exists") {
		t.Fatalf("unexpected duplicate registration error: %q", message)
	}
}

func TestGetToolResolvesOtherToolsAfterFailedRegistration(t *testing.T) {
	reg := NewRegistry()
	reg.SafeRegisterTool(fakeTool{id: "tool.alpha"})
	reg.SafeRegisterTool(nil)
	reg.SafeRegisterTool(fakeTool{id: "   "})

	tool, err := reg.GetTool("tool.alpha")
	if err != nil {
		t.Fatalf("a failed registration must not block other tools: %v", err)
	}
	if tool.ID() != "tool.alpha" {
		t.Fatalf("GetTool resolved %q", tool.ID())
	}

	if _, err := reg.GetTool("tool.missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("an unknown tool must report not found, got %v", err)
	}

	errs := reg.GetInitializationErrors()
	if len(errs) != 2 {
		t.Fatalf("expected two registration errors, got %v", errs)
	}
	if message := errs[0].Error(); !strings.Contains(message, "cannot be nil") {
		t.Fatalf("unexpected nil registration error: %q", message)
	}
	if message := errs[1].Error(); !strings.Contains(message, "id cannot be empty") {
		t.Fatalf("unexpected empty-id registration error: %q", message)
	}
}
