package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEncoderListKeepsOnlyAudioEncoders(t *testing.T) {
	output := `
Encoders:
 V..... libx264              H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10 (codec h264)
 A..... libmp3lame           libmp3lame MP3 (MPEG audio layer 3) (codec mp3)
 A..... aac                  AAC (Advanced Audio Coding)
 A....D libopus              libopus Opus (codec opus)
 A..... pcm_s16le            PCM signed 16-bit little-endian
 A..... flac                 FLAC (Free Lossless Audio Codec)
 S..... srt                  SubRip subtitle
 ------ 
`
	encoders := ParseEncoderList(output)
	for _, want := range []string{"libmp3lame", "aac", "libopus", "pcm_s16le", "flac"} {
		if _, ok := encoders[want]; !ok {
			t.Fatalf("expected encoder %q in %v", want, encoders)
		}
	}
	for _, unwanted := range []string{"libx264", "srt", "Encoders:", "------"} {
		if _, ok := encoders[unwanted]; ok {
			t.Fatalf("did not expect %q in the audio encoder set", unwanted)
		}
	}
}

func TestFormatResolution(t *testing.T) {
	format, err := FormatByName("")
	if err != nil || format.Name != FormatMP3 {
		t.Fatalf("expected mp3 default, got %+v err=%v", format, err)
	}

	format, err = FormatByName("FLAC")
	if err != nil || format.Name != FormatFLAC || !format.Lossless {
		t.Fatalf("expected lossless flac, got %+v err=%v", format, err)
	}

	if _, err := FormatByName("ogg"); err == nil || err.Code != ErrorCodeFormatUnsupported {
		t.Fatalf("expected unsupported format, got %v", err)
	}

	if got := TargetFormatForInput("clip.OGG"); got.Name != FormatOpus {
		t.Fatalf("expected ogg to map to opus, got %q", got.Name)
	}
	if got := TargetFormatForInput("clip.aac"); got.Name != FormatM4A {
		t.Fatalf("expected aac to map to m4a, got %q", got.Name)
	}
	if got := TargetFormatForInput("clip.wma"); got.Name != FormatMP3 {
		t.Fatalf("expected wma to fall back to mp3, got %q", got.Name)
	}
	if got := TargetFormatForInput("clip.m4a"); got.Name != FormatM4A {
		t.Fatalf("expected m4a to be preserved, got %q", got.Name)
	}
}

func TestBuildArgsUsesEncoderAndDropsVideo(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(input, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	output := filepath.Join(dir, "clip.mp3")

	args, err := BuildArgs(Request{InputPath: input, OutputPath: output, Format: FormatMP3})
	if err != nil {
		t.Fatalf("build args: %v", err)
	}
	for _, want := range [][]string{
		{"-hide_banner", "-loglevel", "error", "-nostats", "-nostdin"},
		{"-vn"},
		{"-c:a", "libmp3lame"},
		{"-b:a", "192k"},
	} {
		if !containsSequence(args, want...) {
			t.Fatalf("expected %v in %v", want, args)
		}
	}
	if args[len(args)-1] != output {
		t.Fatalf("expected output last, got %q", args[len(args)-1])
	}
}

func TestBuildArgsLosslessIgnoresBitrate(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	if err := os.WriteFile(input, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	args, err := BuildArgs(Request{InputPath: input, OutputPath: filepath.Join(dir, "clip.flac"), Format: FormatFLAC, BitrateKbps: 320})
	if err != nil {
		t.Fatalf("build args: %v", err)
	}
	if containsSequence(args, "-b:a") {
		t.Fatalf("lossless output must not carry a bitrate, got %v", args)
	}
	if !containsSequence(args, "-c:a", "flac") {
		t.Fatalf("expected the flac encoder, got %v", args)
	}
}

func TestValidateRequestRejectsFormatMismatch(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	if err := os.WriteFile(input, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	err := ValidateRequest(Request{InputPath: input, OutputPath: filepath.Join(dir, "clip.opus"), Format: FormatMP3})
	if err == nil || err.Code != ErrorCodeFormatMismatch {
		t.Fatalf("expected a format mismatch, got %v", err)
	}
}

func TestParseLoudnormReport(t *testing.T) {
	output := `[Parsed_loudnorm_0 @ 0x7f95b4003900] 
{
	"input_i" : "-21.75",
	"input_tp" : "-18.06",
	"input_lra" : "0.00",
	"input_thresh" : "-31.75",
	"output_i" : "-14.05",
	"output_tp" : "-10.31",
	"output_lra" : "0.00",
	"output_thresh" : "-24.05",
	"normalization_type" : "linear",
	"target_offset" : "0.05"
}
size=      48KiB time=00:00:02.00 bitrate= 198.3kbits/s speed=39.5x`

	result, ok := ParseLoudnormReport(output)
	if !ok {
		t.Fatal("expected the report to parse")
	}
	if result.InputLUFS != -21.75 || result.OutputLUFS != -14.05 {
		t.Fatalf("unexpected loudness values: %+v", result)
	}
	if result.InputTruePeak != -18.06 || result.OutputTruePeak != -10.31 {
		t.Fatalf("unexpected true peak values: %+v", result)
	}
	if result.NormalizationType != "linear" {
		t.Fatalf("expected linear normalization, got %q", result.NormalizationType)
	}

	if _, ok := ParseLoudnormReport("no report here"); ok {
		t.Fatal("expected malformed output to be rejected")
	}
}

func TestResolveLoudnormTargetAndTruePeak(t *testing.T) {
	tests := map[string]float64{
		"":          -14,
		"streaming": -14,
		"broadcast": -23,
		"podcast":   -16,
		"-23":       -23,
	}
	for raw, want := range tests {
		target, err := ResolveLoudnormTarget(raw)
		if err != nil {
			t.Fatalf("ResolveLoudnormTarget(%q): %v", raw, err)
		}
		if target.IntegratedLoudness != want {
			t.Fatalf("ResolveLoudnormTarget(%q) = %.0f, want %.0f", raw, target.IntegratedLoudness, want)
		}
	}
	if _, err := ResolveLoudnormTarget("loud"); err == nil || err.Code != ErrorCodeTargetLufsInvalid {
		t.Fatalf("expected an invalid target error, got %v", err)
	}

	for _, valid := range []float64{0, -1.5, -9} {
		if err := ValidateTruePeak(valid); err != nil {
			t.Fatalf("expected %v to be a valid true peak: %v", valid, err)
		}
	}
	for _, invalid := range []float64{1, -10} {
		if err := ValidateTruePeak(invalid); err == nil || err.Code != ErrorCodeTruePeakInvalid {
			t.Fatalf("expected %v to be rejected, got %v", invalid, err)
		}
	}
}

func TestApplyFilterCarriesMeasuredValues(t *testing.T) {
	target, _ := ResolveLoudnormTarget("podcast")
	report := loudnormReport{
		InputI:       "-20.5",
		InputTP:      "-1.2",
		InputLRA:     "4.0",
		InputThresh:  "-30.0",
		TargetOffset: "0.10",
	}
	filter := BuildLoudnormApplyFilter(target, -2, report)
	for _, want := range []string{
		"I=-16.0",
		"TP=-2.0",
		"measured_I=-20.5",
		"measured_TP=-1.2",
		"measured_LRA=4.0",
		"measured_thresh=-30.0",
		"offset=0.10",
		"linear=true",
		"print_format=json",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("expected %q in %q", want, filter)
		}
	}
}

func containsSequence(args []string, want ...string) bool {
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
