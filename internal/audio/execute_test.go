package audio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractConvertAndNormalizeProduceValidAudio(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()

	videoInput := filepath.Join(dir, "clip.mp4")
	generateVideoWithAudio(t, videoInput, 2)

	wavInput := filepath.Join(dir, "sine.wav")
	generateSineWAV(t, wavInput, 2)

	probe, encoders := planDeps()

	t.Run("extract mp3 from video", func(t *testing.T) {
		tool := NewExtractToolWithDeps(probe, nil, encoders)
		item, itemErr := tool.ExecuteSingle(context.Background(),
			audioToolRequest(ToolIDAudioExtract, "single", []string{videoInput}, map[string]any{"format": "mp3"}))
		if itemErr != nil {
			t.Fatalf("extract failed: %v", itemErr)
		}
		if !item.Success {
			t.Fatalf("expected success, got %+v", item)
		}
		if want := filepath.Join(dir, "clip_audio.mp3"); item.OutputPath != want {
			t.Fatalf("expected output %q, got %q", want, item.OutputPath)
		}
		probed := probeAudioOutput(t, item.OutputPath)
		if probed.Codec != "mp3" {
			t.Fatalf("expected mp3 codec, got %q", probed.Codec)
		}
		assertDurationNear(t, probed.DurationSeconds, 2)
	})

	t.Run("convert wav to opus", func(t *testing.T) {
		tool := NewConvertToolWithDeps(probe, nil, encoders)
		item, itemErr := tool.ExecuteSingle(context.Background(),
			audioToolRequest(ToolIDAudioConvert, "single", []string{wavInput}, map[string]any{"format": "opus"}))
		if itemErr != nil {
			t.Fatalf("convert failed: %v", itemErr)
		}
		if want := filepath.Join(dir, "sine_converted.opus"); item.OutputPath != want {
			t.Fatalf("expected output %q, got %q", want, item.OutputPath)
		}
		probed := probeAudioOutput(t, item.OutputPath)
		if probed.Codec != "opus" {
			t.Fatalf("expected opus codec, got %q", probed.Codec)
		}
		assertDurationNear(t, probed.DurationSeconds, 2)
	})

	t.Run("normalize wav preserving the container", func(t *testing.T) {
		tool := NewNormalizeToolWithDeps(probe, nil, encoders)
		item, itemErr := tool.ExecuteSingle(context.Background(),
			audioToolRequest(ToolIDAudioNormalize, "single", []string{wavInput}, map[string]any{"targetLufs": "broadcast"}))
		if itemErr != nil {
			t.Fatalf("normalize failed: %v", itemErr)
		}
		if want := filepath.Join(dir, "sine_normalized.wav"); item.OutputPath != want {
			t.Fatalf("expected output %q, got %q", want, item.OutputPath)
		}
		probed := probeAudioOutput(t, item.OutputPath)
		if probed.Codec != "pcm_s16le" {
			t.Fatalf("expected pcm_s16le codec, got %q", probed.Codec)
		}
		assertDurationNear(t, probed.DurationSeconds, 2)

		inputLufs, _ := item.Details["inputLufs"].(float64)
		outputLufs, _ := item.Details["outputLufs"].(float64)
		if inputLufs >= 0 || outputLufs >= 0 {
			t.Fatalf("expected measured loudness in the result item, got %+v", item.Details)
		}
		if !strings.Contains(item.Message, "LUFS") {
			t.Fatalf("expected the result message to report loudness, got %q", item.Message)
		}
	})
}

func TestBatchReportsPartialSuccess(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()

	good := filepath.Join(dir, "good.wav")
	generateSineWAV(t, good, 1)
	bad := filepath.Join(dir, "bad.wav")
	if err := os.WriteFile(bad, []byte("not an audio file"), 0o644); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}

	probe, encoders := planDeps()
	tool := NewConvertToolWithDeps(probe, nil, encoders)
	items, batchErr := tool.ExecuteBatch(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "batch", []string{good, bad}, nil), nil)
	if batchErr == nil {
		t.Fatal("expected the batch to report the failed item")
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !items[0].Success {
		t.Fatalf("expected the good input to succeed, got %+v", items[0])
	}
	if items[1].Success || items[1].Error == nil {
		t.Fatalf("expected the bad input to fail with an error, got %+v", items[1])
	}
	if _, statErr := os.Stat(items[0].OutputPath); statErr != nil {
		t.Fatalf("expected the good output: %v", statErr)
	}
	if _, statErr := os.Stat(items[1].OutputPath); !os.IsNotExist(statErr) {
		t.Fatalf("failed item must not publish an output, stat err: %v", statErr)
	}
}

func TestCancellationStopsTheEncodeAndLeavesNoOrphan(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()

	input := filepath.Join(dir, "long.mp3")
	generateLongSineMP3(t, input, 300)

	probe, encoders := planDeps()
	tool := NewConvertToolWithDeps(probe, nil, encoders)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	item, itemErr := tool.ExecuteSingle(ctx,
		audioToolRequest(ToolIDAudioConvert, "single", []string{input}, map[string]any{"format": "mp3", "bitrateKbps": 320}))
	if itemErr == nil {
		t.Skip("the encode finished before the timeout; the machine is too fast for this cancellation check")
	}
	code := strings.ToUpper(itemErr.Code + " " + itemErr.DetailCode)
	if !strings.Contains(code, "CANCEL") {
		t.Fatalf("expected a cancellation error, got %+v", itemErr)
	}
	if item.OutputPath != "" {
		if _, statErr := os.Stat(item.OutputPath); !os.IsNotExist(statErr) {
			t.Fatalf("cancelled output must not exist, stat err: %v", statErr)
		}
	}
	assertNoOrphanFFmpeg(t, input)

	matches, globErr := filepath.Glob(filepath.Join(dir, ".fileforge-*"))
	if globErr != nil {
		t.Fatalf("glob temp outputs: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("cancellation left temp outputs behind: %v", matches)
	}
}

func assertDurationNear(t *testing.T, got, want float64) {
	t.Helper()

	if got < want-0.3 || got > want+0.3 {
		t.Fatalf("duration = %.3f, want %.1f ± 0.3", got, want)
	}
}
