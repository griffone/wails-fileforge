package registry

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"fileforge-desktop/internal/models"
)

type registryTestTool struct {
	id         string
	capability string
}

func (t *registryTestTool) ID() string { return t.id }

func (t *registryTestTool) Capability() string { return t.capability }

func (t *registryTestTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:         t.id,
		Name:           t.id,
		Capability:     t.capability,
		Domain:         "test",
		Version:        "v1",
		SupportsSingle: true,
		SupportsBatch:  true,
	}
}

func (t *registryTestTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *registryTestTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func TestRegistryListToolsV2ReturnsStableSortedOrder(t *testing.T) {
	r := NewRegistry()

	tools := []*registryTestTool{
		{id: "tool.z", capability: "cap.a"},
		{id: "tool.a", capability: "cap.a"},
		{id: "tool.m", capability: "cap.b"},
	}

	for _, tool := range tools {
		if err := r.RegisterToolV2(tool); err != nil {
			t.Fatalf("register failed: %v", err)
		}
	}

	catalog := r.ListToolsV2(context.Background())
	ids := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		ids = append(ids, entry.Manifest.ToolID)
	}

	expected := []string{"tool.a", "tool.m", "tool.z"}
	for i := range expected {
		if ids[i] != expected[i] {
			t.Fatalf("expected sorted ids %v, got %v", expected, ids)
		}
	}

	capCatalog := r.GetToolsByCapabilityV2("cap.a")
	capIDs := make([]string, 0, len(capCatalog))
	for _, entry := range capCatalog {
		capIDs = append(capIDs, entry.Manifest.ToolID)
	}

	expectedCapIDs := []string{"tool.a", "tool.z"}
	for i := range expectedCapIDs {
		if capIDs[i] != expectedCapIDs[i] {
			t.Fatalf("expected capability sorted ids %v, got %v", expectedCapIDs, capIDs)
		}
	}
}

func TestRegistryConcurrentRegisterAndListToolsV2(t *testing.T) {
	r := NewRegistry()
	const total = 50

	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			tool := &registryTestTool{id: fmt.Sprintf("tool.%03d", index), capability: "cap.concurrent"}
			if err := r.RegisterToolV2(tool); err != nil {
				t.Errorf("register failed: %v", err)
			}
		}(i)
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.ListToolsV2(context.Background())
			_ = r.GetToolsByCapabilityV2("cap.concurrent")
		}()
	}

	wg.Wait()

	if got := r.CountToolsV2(); got != total {
		t.Fatalf("expected %d tools, got %d", total, got)
	}

	catalog := r.ListToolsV2(context.Background())
	ids := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		ids = append(ids, entry.Manifest.ToolID)
	}

	sortedIDs := make([]string, len(ids))
	copy(sortedIDs, ids)
	sort.Strings(sortedIDs)
	for i := range ids {
		if ids[i] != sortedIDs[i] {
			t.Fatalf("catalog not returned in stable sorted order")
		}
	}
}
