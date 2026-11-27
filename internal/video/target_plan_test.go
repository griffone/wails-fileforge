package video

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/video/engine"
)

func stubPlanTargetMedia(t *testing.T, duration float64, height int, hasAudio bool) {
	t.Helper()

	original := probeTargetMedia
	t.Cleanup(func() {
		probeTargetMedia = original
	})

	probeTargetMedia = func(context.Context, string) (engine.TargetMedia, bool) {
		return engine.TargetMedia{DurationSeconds: duration, Height: height, HasAudio: hasAudio}, true
	}
}

func stubExecutionTargetMedia(t *testing.T, duration float64, height int, hasAudio bool) {
	t.Helper()

	original := engine.TargetMediaProbe
	t.Cleanup(func() {
		engine.TargetMediaProbe = original
	})

	engine.TargetMediaProbe = func(context.Context, string) (engine.TargetMedia, bool) {
		return engine.TargetMedia{DurationSeconds: duration, Height: height, HasAudio: hasAudio}, true
	}
}

func TestConvertPlanTargetWarningsAndEffectiveOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	stubPlanTargetMedia(t, 10, 1080, true)

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, map[string]any{
		"targetSizeBytes": 10_000_000,
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if got := plan.EffectiveOptions["targetSizeBytes"]; got != int64(10_000_000) {
		t.Fatalf("effective target = %v (%T), want int64(10000000)", got, got)
	}

	warnings := strings.Join(plan.Warnings, "\n")
	for _, want := range []string{"two-pass", "best effort", "twice as long", "1080p"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("expected %q in warnings, got %q", want, warnings)
		}
	}
	if strings.Contains(warnings, "floor is applied") {
		t.Fatalf("a reachable target must not warn about the floor: %q", warnings)
	}
}

func TestConvertPlanTargetBelowFloorWarns(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	stubPlanTargetMedia(t, 10, 720, true)

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, map[string]any{
		"targetSizeBytes": 100_000,
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	warnings := strings.Join(plan.Warnings, "\n")
	for _, want := range []string{"floor is applied", "may not be reachable", "1000 kbps"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("expected %q in warnings, got %q", want, warnings)
		}
	}
}

func TestConvertPlanTargetOverridesExplicitBitrateWarns(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	stubPlanTargetMedia(t, 10, 1080, true)

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, map[string]any{
		"targetSizeBytes": 10_000_000,
		"bitrateKbps":     3200,
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if warnings := strings.Join(plan.Warnings, "\n"); !strings.Contains(warnings, "overrides bitrateKbps") {
		t.Fatalf("expected the target to explain it overrides bitrateKbps, got %q", warnings)
	}
}

func TestTrimPlanTargetForcesReencode(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	stubVideoDuration(t, 10, true)
	stubPlanTargetMedia(t, 10, 720, true)

	options := trimOptions(1, 5)
	options["trimMode"] = engine.TrimModeCopy
	options["targetSizeBytes"] = 5_000_000

	plan, planErr := newTrimPlanTool(t).Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if got := plan.EffectiveOptions["trimMode"]; got != engine.TrimModeReencode {
		t.Fatalf("trimMode = %v, want %q", got, engine.TrimModeReencode)
	}
	if warnings := strings.Join(plan.Warnings, "\n"); !strings.Contains(warnings, "copy mode cannot meet a size target") {
		t.Fatalf("expected the copy-mode switch warning, got %q", warnings)
	}
}

func TestMergePlanTargetForcesReencodeAndWarnsApproximation(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)
	stubPlanTargetMedia(t, 5, 720, true)

	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMerge, "single", []string{first, second}, map[string]any{
		"targetSizeBytes": 5_000_000,
		"mergeMode":       engine.MergeModeAuto,
		"transition":      engine.MergeTransitionCrossfade,
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if got := plan.EffectiveOptions["mergeMode"]; got != engine.MergeModeReencode {
		t.Fatalf("mergeMode = %v, want %q", got, engine.MergeModeReencode)
	}

	warnings := strings.Join(plan.Warnings, "\n")
	for _, want := range []string{"copy mode cannot meet a size target", "summed input duration", "crossfades shorten", "approximation"} {
		if !strings.Contains(warnings, want) {
			t.Fatalf("expected %q in warnings, got %q", want, warnings)
		}
	}
}

func TestConvertExecuteReportsTargetOutcomeDetails(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	stubPlanTargetMedia(t, 1, 360, false)
	stubExecutionTargetMedia(t, 1, 360, false)

	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, &stubCommandRunner{})
	item, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, map[string]any{
		"targetSizeBytes": 1_000_000,
		"removeAudio":     true,
	}))
	if itemErr != nil {
		t.Fatalf("expected conversion to succeed, got %v", itemErr)
	}
	if !item.Success || item.Details == nil {
		t.Fatalf("expected a success item with target details, got %+v", item)
	}
	if got := item.Details["targetMet"]; got != true {
		t.Fatalf("targetMet = %v, want true", got)
	}
	if got := item.Details["targetBitrateKbps"]; got != 8000 {
		t.Fatalf("targetBitrateKbps = %v, want 8000", got)
	}
	if got := item.Details["targetSizeBytes"]; got != int64(1_000_000) {
		t.Fatalf("targetSizeBytes = %v, want 1000000", got)
	}
	if !strings.Contains(item.Message, "target met") {
		t.Fatalf("expected a met message, got %q", item.Message)
	}
}

func TestConvertExecuteReportsBestEffortWhenFloorApplies(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	stubPlanTargetMedia(t, 1, 360, false)
	stubExecutionTargetMedia(t, 1, 360, false)

	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, &stubCommandRunner{})
	item, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, map[string]any{
		"targetSizeBytes": 3,
		"removeAudio":     true,
	}))
	if itemErr != nil {
		t.Fatalf("expected best-effort conversion to succeed, got %v", itemErr)
	}
	if item.Details == nil {
		t.Fatalf("expected target details, got %+v", item)
	}
	if got := item.Details["targetMet"]; got != false {
		t.Fatalf("targetMet = %v, want false", got)
	}
	if got := item.Details["floorApplied"]; got != true {
		t.Fatalf("floorApplied = %v, want true", got)
	}
	if got := item.Details["floorKbps"]; got != 300 {
		t.Fatalf("floorKbps = %v, want 300", got)
	}
	if !strings.Contains(item.Message, "target not met (best effort)") {
		t.Fatalf("expected an honest best-effort message, got %q", item.Message)
	}
}
