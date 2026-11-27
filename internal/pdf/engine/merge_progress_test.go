package engine

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMergeProgressReportsPerInputCounts(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.pdf"),
		filepath.Join(dir, "second.pdf"),
		filepath.Join(dir, "third.pdf"),
	}
	for _, inputPath := range inputs {
		writeTestPDF(t, inputPath)
	}
	outputPath := filepath.Join(dir, "merged.pdf")

	var events []MergeProgressEvent
	mergeErr := MergeWithPageRangesProgress(context.Background(), inputs, outputPath, nil, func(event MergeProgressEvent) {
		events = append(events, event)
	})
	if mergeErr != nil {
		t.Fatalf("expected the merge to succeed, got %v", mergeErr)
	}

	if len(events) == 0 {
		t.Fatal("expected progress events")
	}
	if first := events[0]; first.Stage != MergeStageValidate || first.Current != 0 || first.Total != len(inputs) {
		t.Fatalf("unexpected first event: %+v", first)
	}
	if last := events[len(events)-1]; last.Stage != MergeStageDone || last.Current != len(inputs) || last.Total != len(inputs) {
		t.Fatalf("unexpected final event: %+v", last)
	}

	previous := 0
	sawPerInput := false
	for index, event := range events {
		if event.Current < previous {
			t.Fatalf("progress regressed from %d to %d: %+v", previous, event.Current, events)
		}
		if event.Current > event.Total {
			t.Fatalf("event exceeds its total: %+v", event)
		}
		if event.Stage == MergeStageValidate && event.Current > 0 && event.Current < event.Total {
			sawPerInput = true
		}
		if index < len(events)-1 && event.Current > 0 && event.Current == event.Total {
			t.Fatalf("progress reached its total before completion: %+v", event)
		}
		previous = event.Current
	}
	if !sawPerInput {
		t.Fatalf("expected per-input counts below the total, got %+v", events)
	}
}

func TestMergeProgressIsSilentWithoutCallback(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestPDF(t, first)
	writeTestPDF(t, second)

	if mergeErr := MergeWithPageRangesProgress(context.Background(), []string{first, second}, filepath.Join(dir, "merged.pdf"), nil, nil); mergeErr != nil {
		t.Fatalf("expected the merge to succeed without a callback, got %v", mergeErr)
	}
}
