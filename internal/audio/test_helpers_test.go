package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fileforge-desktop/internal/audio/engine"
	"fileforge-desktop/internal/models"
)

// stubRuntimeProbe simulates the ffmpeg/ffprobe lookup without touching PATH.
type stubRuntimeProbe struct {
	err error
}

func (p stubRuntimeProbe) Check(context.Context) error {
	return p.err
}

func allAudioEncoders() map[string]struct{} {
	encoders := make(map[string]struct{})
	for _, name := range engine.AllEncoderNames() {
		encoders[name] = struct{}{}
	}

	return encoders
}

func encoderProbe(encoders ...string) *engine.EncoderProbe {
	if len(encoders) == 0 {
		encoders = engine.AllEncoderNames()
	}
	set := make(map[string]struct{}, len(encoders))
	for _, name := range encoders {
		set[name] = struct{}{}
	}

	return engine.NewEncoderProbe(engine.EncoderListerFunc(func(context.Context) (map[string]struct{}, error) {
		return set, nil
	}))
}

func planDeps() (engine.RuntimeProbe, *engine.EncoderProbe) {
	return stubRuntimeProbe{}, encoderProbe()
}

func audioToolRequest(toolID, mode string, inputPaths []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     toolID,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

// writeAudioFixture creates a placeholder file. Planning only stats and probes
// paths, so the bytes never need to be real audio.
func writeAudioFixture(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping real ffmpeg test in short mode")
	}
	for _, binary := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s is not available: %v", binary, err)
		}
	}
}

func generateSineWAV(t *testing.T, path string, seconds float64) {
	t.Helper()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%.3f", seconds),
		"-c:a", "pcm_s16le",
		path,
	}
	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate sine wav: %v (%s)", err, strings.TrimSpace(string(output)))
	}
}

func generateVideoWithAudio(t *testing.T, path string, seconds float64) {
	t.Helper()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi",
		"-i", "testsrc=size=160x120:rate=10",
		"-f", "lavfi",
		"-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%.3f", seconds),
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "ultrafast",
		"-c:a", "aac",
		"-t", fmt.Sprintf("%.3f", seconds),
		"-shortest",
		path,
	}
	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate video with audio: %v (%s)", err, strings.TrimSpace(string(output)))
	}
}

// generateLongSineMP3 creates a small but long-enough input that a short
// context timeout interrupts the conversion instead of racing a quick encode.
func generateLongSineMP3(t *testing.T, path string, seconds float64) {
	t.Helper()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi",
		"-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%.3f", seconds),
		"-c:a", "libmp3lame", "-b:a", "64k",
		path,
	}
	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate long sine mp3: %v (%s)", err, strings.TrimSpace(string(output)))
	}
}

type probedAudio struct {
	Codec           string
	DurationSeconds float64
}

// probeAudioOutput reads the codec and duration of a real output with ffprobe,
// exactly the values the verification layer relies on.
func probeAudioOutput(t *testing.T, path string) probedAudio {
	t.Helper()

	output, err := exec.Command(
		"ffprobe",
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name:format=duration",
		"-of", "json",
		path,
	).Output()
	if err != nil {
		t.Fatalf("probe %s: %v", path, err)
	}

	var parsed struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("parse ffprobe output: %v", err)
	}

	result := probedAudio{}
	for _, stream := range parsed.Streams {
		if stream.CodecType == "audio" && result.Codec == "" {
			result.Codec = stream.CodecName
		}
	}
	fmt.Sscanf(parsed.Format.Duration, "%f", &result.DurationSeconds)

	return result
}

// assertNoOrphanFFmpeg polls for a process whose command line contains the
// unique pattern. pgrep is unix-only; the assertion is skipped where it is
// unavailable.
func assertNoOrphanFFmpeg(t *testing.T, pattern string) {
	t.Helper()

	pgrep, err := exec.LookPath("pgrep")
	if err != nil {
		return
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.Command(pgrep, "-f", "--", pattern).Run(); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("an ffmpeg process is still running for %s", pattern)
}

func hasArgSequence(args []string, want ...string) bool {
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
