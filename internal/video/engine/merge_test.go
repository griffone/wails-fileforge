package engine

import (
	"path/filepath"
	"testing"
)

func mergeTestRequest(t *testing.T) MergeRequest {
	t.Helper()

	dir := t.TempDir()
	return MergeRequest{
		InputPaths:    []string{filepath.Join(dir, "first.mp4"), filepath.Join(dir, "second.mp4")},
		OutputPath:    filepath.Join(dir, "merged.mp4"),
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		MergeMode:     MergeModeReencode,
	}
}

func TestBuildMergeArgsKeepsAudioByDefault(t *testing.T) {
	req := mergeTestRequest(t)
	listFilePath := filepath.Join(t.TempDir(), "list.txt")

	args, buildErr := BuildMergeArgs(listFilePath, req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-f", "concat") {
		t.Fatalf("expected concat input, got %v", args)
	}
	if !containsArgSequence(args, "-c:a", "aac") {
		t.Fatalf("expected the default audio codec, got %v", args)
	}
	if containsArgSequence(args, "-an") {
		t.Fatalf("default merge must keep audio, got %v", args)
	}
	if got := args[len(args)-1]; got != req.OutputPath {
		t.Fatalf("expected output path %q last, got %q", req.OutputPath, got)
	}
}

func TestBuildMergeArgsRemoveAudioDropsAudioCodec(t *testing.T) {
	req := mergeTestRequest(t)
	req.RemoveAudio = true
	listFilePath := filepath.Join(t.TempDir(), "list.txt")

	args, buildErr := BuildMergeArgs(listFilePath, req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an for removeAudio, got %v", args)
	}
	if containsArgSequence(args, "-c:a") {
		t.Fatalf("removeAudio must not add an audio codec, got %v", args)
	}
	if got := args[len(args)-1]; got != req.OutputPath {
		t.Fatalf("expected output path %q last, got %q", req.OutputPath, got)
	}
}

func TestBuildMergeArgsWebMRemoveAudioDropsOpus(t *testing.T) {
	req := mergeTestRequest(t)
	req.TargetFormat = TargetFormatWebM
	req.OutputPath = filepath.Join(filepath.Dir(req.OutputPath), "merged.webm")
	req.RemoveAudio = true
	listFilePath := filepath.Join(t.TempDir(), "list.txt")

	args, buildErr := BuildMergeArgs(listFilePath, req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-c:v", "libvpx-vp9") {
		t.Fatalf("expected the webm video codec, got %v", args)
	}
	if containsArgSequence(args, "libopus") {
		t.Fatalf("removeAudio must not add an opus codec, got %v", args)
	}
	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an for removeAudio, got %v", args)
	}
}

func TestBuildMergeCopyArgsRemoveAudioDropsAudioStreams(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "merged.mp4")

	args := buildMergeCopyArgs("/tmp/list.txt", outputPath, true)

	if !containsArgSequence(args, "-c", "copy") {
		t.Fatalf("expected stream copy, got %v", args)
	}
	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an for removeAudio, got %v", args)
	}
	if got := args[len(args)-1]; got != outputPath {
		t.Fatalf("expected output path %q last, got %q", outputPath, got)
	}
}

func TestBuildMergeArgsRejectsInvalidQualityPreset(t *testing.T) {
	req := mergeTestRequest(t)
	req.QualityPreset = "ultra"

	_, buildErr := BuildMergeArgs(filepath.Join(t.TempDir(), "list.txt"), req)
	if buildErr == nil {
		t.Fatal("expected a validation error")
	}
	if buildErr.Code != ErrorCodeVideoMergeValidation {
		t.Fatalf("expected %q, got %q", ErrorCodeVideoMergeValidation, buildErr.Code)
	}
}
