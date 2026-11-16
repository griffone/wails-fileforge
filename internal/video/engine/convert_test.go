package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConvertFixtures creates a placeholder input and a free output path. The
// builders only stat paths, so the bytes never need to be a real video.
func writeConvertFixtures(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input fixture: %v", err)
	}

	return inputPath, filepath.Join(dir, "clip_converted.mp4")
}

func convertTestRequest(inputPath, outputPath string) ConvertRequest {
	return ConvertRequest{
		InputPath:     inputPath,
		OutputPath:    outputPath,
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		Resolution:    ResolutionOriginal,
	}
}

func containsArgSequence(args []string, want ...string) bool {
	if len(want) == 0 {
		return true
	}

	for index := 0; index+len(want) <= len(args); index++ {
		matched := true
		for offset, value := range want {
			if args[index+offset] != value {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

func TestBuildConvertArgsKeepsPresetDefaultsForOriginalResolution(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)

	args, buildErr := BuildConvertArgs(convertTestRequest(inputPath, outputPath))
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	for _, want := range [][]string{
		{"-i", inputPath},
		{"-c:v", "libx264"},
		{"-b:v", "2500k"},
		{"-crf", "23"},
		{"-c:a", "aac"},
	} {
		if !containsArgSequence(args, want...) {
			t.Fatalf("expected %v in args, got %v", want, args)
		}
	}

	if containsArgSequence(args, "-vf") {
		t.Fatalf("original resolution must not add a scale filter, got %v", args)
	}
	if containsArgSequence(args, "-an") {
		t.Fatalf("original audio must not be dropped, got %v", args)
	}
	if got := args[len(args)-1]; got != outputPath {
		t.Fatalf("expected output path %q last, got %q", outputPath, got)
	}
}

func TestBuildConvertArgsAppliesResolution(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)

	req := convertTestRequest(inputPath, outputPath)
	req.Resolution = Resolution720p

	args, buildErr := BuildConvertArgs(req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-vf", "scale=-2:720") {
		t.Fatalf("expected a -2:720 scale filter, got %v", args)
	}
	if got := args[len(args)-1]; got != outputPath {
		t.Fatalf("expected output path %q last, got %q", outputPath, got)
	}
}

func TestBuildConvertArgsUsesExplicitBitrateWithoutCRF(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)

	req := convertTestRequest(inputPath, outputPath)
	req.BitrateKbps = 3200

	args, buildErr := BuildConvertArgs(req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-b:v", "3200k") {
		t.Fatalf("expected the explicit bitrate, got %v", args)
	}
	if containsArgSequence(args, "-crf") {
		t.Fatalf("explicit bitrate must omit -crf, got %v", args)
	}
}

func TestBuildConvertArgsRemoveAudioDropsAudioCodec(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)

	req := convertTestRequest(inputPath, outputPath)
	req.RemoveAudio = true

	args, buildErr := BuildConvertArgs(req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an for removeAudio, got %v", args)
	}
	if containsArgSequence(args, "-c:a") {
		t.Fatalf("removeAudio must not add an audio codec, got %v", args)
	}
}

func TestBuildConvertArgsTreatsEmptyResolutionAsOriginal(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)

	req := convertTestRequest(inputPath, outputPath)
	req.Resolution = ""

	args, buildErr := BuildConvertArgs(req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}
	if containsArgSequence(args, "-vf") {
		t.Fatalf("empty resolution must keep the original size, got %v", args)
	}
}

func TestBuildConvertArgsWebMExplicitBitrateWithoutCRF(t *testing.T) {
	inputPath, _ := writeConvertFixtures(t)
	outputPath := filepath.Join(filepath.Dir(inputPath), "clip_converted.webm")

	req := convertTestRequest(inputPath, outputPath)
	req.TargetFormat = TargetFormatWebM
	req.BitrateKbps = 1800
	req.RemoveAudio = true

	args, buildErr := BuildConvertArgs(req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-c:v", "libvpx-vp9") {
		t.Fatalf("expected the webm video codec, got %v", args)
	}
	if !containsArgSequence(args, "-b:v", "1800k") {
		t.Fatalf("expected the explicit bitrate, got %v", args)
	}
	if containsArgSequence(args, "-crf") {
		t.Fatalf("explicit bitrate must omit -crf, got %v", args)
	}
	if containsArgSequence(args, "-c:a") {
		t.Fatalf("removeAudio must not add an audio codec, got %v", args)
	}
}

func TestValidateConvertRequestRejectsInvalidOutputControls(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)

	cases := []struct {
		name   string
		mutate func(*ConvertRequest)
	}{
		{name: "invalid resolution", mutate: func(req *ConvertRequest) { req.Resolution = "4k" }},
		{name: "negative bitrate", mutate: func(req *ConvertRequest) { req.BitrateKbps = -1 }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			req := convertTestRequest(inputPath, outputPath)
			testCase.mutate(&req)

			validationErr := ValidateConvertRequest(req)
			if validationErr == nil {
				t.Fatal("expected a validation error")
			}
			if validationErr.Code != ErrorCodeVideoValidation {
				t.Fatalf("expected %q, got %q", ErrorCodeVideoValidation, validationErr.Code)
			}
		})
	}
}
