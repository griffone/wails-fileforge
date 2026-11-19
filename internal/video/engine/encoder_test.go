package engine

import (
	"strconv"
	"strings"
	"testing"
)

func TestResolveVideoEncodingTiers(t *testing.T) {
	tests := []struct {
		name         string
		targetFormat string
		preset       string
		wantCodec    string
		wantCRF      string
		wantBitrate  string
		wantCPUUsed  int
		wantX264     string
	}{
		{
			name:         "webm high",
			targetFormat: TargetFormatWebM,
			preset:       QualityPresetHigh,
			wantCodec:    "libvpx-vp9",
			wantCRF:      "28",
			wantBitrate:  "2600k",
			wantCPUUsed:  vp9CPUUsedHigh,
		},
		{
			name:         "webm medium",
			targetFormat: TargetFormatWebM,
			preset:       QualityPresetMedium,
			wantCodec:    "libvpx-vp9",
			wantCRF:      "32",
			wantBitrate:  "1800k",
			wantCPUUsed:  vp9CPUUsedMedium,
		},
		{
			name:         "webm low",
			targetFormat: TargetFormatWebM,
			preset:       QualityPresetLow,
			wantCodec:    "libvpx-vp9",
			wantCRF:      "36",
			wantBitrate:  "1200k",
			wantCPUUsed:  vp9CPUUsedLow,
		},
		{
			name:         "mp4 high",
			targetFormat: TargetFormatMP4,
			preset:       QualityPresetHigh,
			wantCodec:    "libx264",
			wantCRF:      "20",
			wantBitrate:  "4500k",
			wantX264:     x264PresetHigh,
		},
		{
			name:         "mp4 medium",
			targetFormat: TargetFormatMP4,
			preset:       QualityPresetMedium,
			wantCodec:    "libx264",
			wantCRF:      "23",
			wantBitrate:  "2500k",
			wantX264:     x264PresetMedium,
		},
		{
			name:         "mp4 low",
			targetFormat: TargetFormatMP4,
			preset:       QualityPresetLow,
			wantCodec:    "libx264",
			wantCRF:      "28",
			wantBitrate:  "1400k",
			wantX264:     x264PresetLow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoding, err := resolveVideoEncoding(test.targetFormat, test.preset)
			if err != nil {
				t.Fatalf("expected encoding, got %v", err)
			}
			if encoding.videoCodec != test.wantCodec {
				t.Fatalf("codec: got %q want %q", encoding.videoCodec, test.wantCodec)
			}
			if encoding.videoCRF != test.wantCRF {
				t.Fatalf("crf: got %q want %q", encoding.videoCRF, test.wantCRF)
			}
			if encoding.videoBitrate != test.wantBitrate {
				t.Fatalf("bitrate: got %q want %q", encoding.videoBitrate, test.wantBitrate)
			}
			if encoding.vp9CPUUsed != test.wantCPUUsed {
				t.Fatalf("cpu-used: got %d want %d", encoding.vp9CPUUsed, test.wantCPUUsed)
			}
			if encoding.x264Preset != test.wantX264 {
				t.Fatalf("x264 preset: got %q want %q", encoding.x264Preset, test.wantX264)
			}
		})
	}
}

func TestResolveVideoEncodingRejectsInvalidOptions(t *testing.T) {
	if _, err := resolveVideoEncoding("gif", QualityPresetHigh); err == nil {
		t.Fatal("expected an invalid target format error")
	}
	if _, err := resolveVideoEncoding(TargetFormatMP4, "ultra"); err == nil {
		t.Fatal("expected an invalid quality preset error")
	}
}

func TestAppendVideoEncodingArgsTunesVP9(t *testing.T) {
	encoding, err := resolveVideoEncoding(TargetFormatWebM, QualityPresetHigh)
	if err != nil {
		t.Fatalf("expected encoding, got %v", err)
	}

	args := appendVideoEncodingArgs([]string{"-y"}, encoding, false)
	for _, want := range [][]string{
		{"-c:v", "libvpx-vp9"},
		{"-crf", "28"},
		{"-row-mt", "1"},
		{"-cpu-used", strconv.Itoa(vp9CPUUsedHigh)},
		{"-deadline", "realtime"},
		{"-threads", strconv.Itoa(vp9ThreadCount())},
		{"-pix_fmt", "yuv420p"},
		{"-c:a", "libopus"},
		{"-b:a", audioBitrateOpus},
	} {
		if !containsArgSequence(args, want...) {
			t.Fatalf("expected %v in args, got %v", want, args)
		}
	}
}

func TestAppendVideoEncodingArgsUsesX264PresetTier(t *testing.T) {
	encoding, err := resolveVideoEncoding(TargetFormatMP4, QualityPresetLow)
	if err != nil {
		t.Fatalf("expected encoding, got %v", err)
	}

	args := appendVideoEncodingArgs(nil, encoding, false)
	if !containsArgSequence(args, "-preset", x264PresetLow) {
		t.Fatalf("expected the low x264 preset, got %v", args)
	}
	if !containsArgSequence(args, "-movflags", "+faststart") {
		t.Fatalf("expected faststart for mp4, got %v", args)
	}
	if !containsArgSequence(args, "-pix_fmt", "yuv420p") {
		t.Fatalf("expected a browser-safe pixel format, got %v", args)
	}
}

func TestAppendVideoEncodingArgsBitrateOnlyOmitsCRF(t *testing.T) {
	encoding, err := resolveVideoEncoding(TargetFormatMP4, QualityPresetHigh)
	if err != nil {
		t.Fatalf("expected encoding, got %v", err)
	}

	args := appendVideoEncodingArgs(nil, encoding.withBitrateOnly(3200), false)
	if !containsArgSequence(args, "-b:v", "3200k") {
		t.Fatalf("expected the explicit bitrate, got %v", args)
	}
	if containsArgSequence(args, "-crf") {
		t.Fatalf("bitrate-only mode must omit -crf, got %v", args)
	}
}

func TestAppendVideoEncodingArgsRemoveAudio(t *testing.T) {
	encoding, err := resolveVideoEncoding(TargetFormatWebM, QualityPresetMedium)
	if err != nil {
		t.Fatalf("expected encoding, got %v", err)
	}

	args := appendVideoEncodingArgs(nil, encoding, true)
	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an, got %v", args)
	}
	if containsArgSequence(args, "-c:a") {
		t.Fatalf("removeAudio must not add an audio codec, got %v", args)
	}
}

func TestVP9ThreadCountStaysPositive(t *testing.T) {
	if got := vp9ThreadCount(); got < 1 || got > vp9MaxThreads {
		t.Fatalf("unexpected vp9 thread count: %d", got)
	}
}

func TestEncoderCodecStringsAreStable(t *testing.T) {
	encoding, err := resolveVideoEncoding(TargetFormatWebM, QualityPresetHigh)
	if err != nil {
		t.Fatalf("expected encoding, got %v", err)
	}
	if !strings.Contains(encoding.videoCodec, "vp9") {
		t.Fatalf("expected a vp9 codec, got %q", encoding.videoCodec)
	}
}
