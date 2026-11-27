package engine

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const mixIntegrationSampleRate = 48000

// requireFFmpeg skips a test when the host cannot run the real media runtime.
func requireFFmpeg(t *testing.T) (string, string) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping real ffmpeg audio sync tests in short mode")
	}
	ffmpegPath, ffmpegErr := exec.LookPath("ffmpeg")
	if ffmpegErr != nil {
		t.Skipf("ffmpeg is not available: %v", ffmpegErr)
	}
	ffprobePath, ffprobeErr := exec.LookPath("ffprobe")
	if ffprobeErr != nil {
		t.Skipf("ffprobe is not available: %v", ffprobeErr)
	}

	return ffmpegPath, ffprobePath
}

// generateAudioSyncVideo writes a short H.264 clip whose own audio is a known
// tone, so a leaked original branch is detectable by frequency.
func generateAudioSyncVideo(t *testing.T, ffmpegPath, path string, durationSeconds float64, frequency int) {
	t.Helper()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=%g:size=160x120:rate=10", durationSeconds),
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=%g", frequency, durationSeconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-shortest",
		path,
	}
	runAudioSyncCommand(t, ffmpegPath, args)
}

// generateAudioSyncSource writes an audio-only recording with a known tone.
func generateAudioSyncSource(t *testing.T, ffmpegPath, path string, durationSeconds float64, frequency int) {
	t.Helper()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=%d:duration=%g", frequency, durationSeconds),
		"-c:a", "aac", "-b:a", "128k",
		path,
	}
	runAudioSyncCommand(t, ffmpegPath, args)
}

func runAudioSyncCommand(t *testing.T, path string, args []string) {
	t.Helper()

	output, err := exec.Command(path, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

// audioSyncProbe is the stream and format information the integration checks
// assert on.
type audioSyncProbe struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func probeAudioSyncOutput(t *testing.T, ffprobePath, path string) (videoCodec string, videoCount, audioCount int, duration float64) {
	t.Helper()

	output, err := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name:format=duration",
		"-of", "json",
		path,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe failed: %v", err)
	}

	var probe audioSyncProbe
	if err := json.Unmarshal(output, &probe); err != nil {
		t.Fatalf("decode ffprobe json: %v", err)
	}

	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			videoCount++
			if videoCodec == "" {
				videoCodec = stream.CodecName
			}
		case "audio":
			audioCount++
		}
	}

	duration, err = strconv.ParseFloat(strings.TrimSpace(probe.Format.Duration), 64)
	if err != nil {
		t.Fatalf("parse duration %q: %v", probe.Format.Duration, err)
	}

	return videoCodec, videoCount, audioCount, duration
}

func probeAudioSyncChannels(t *testing.T, ffprobePath, path string) int {
	t.Helper()

	output, err := exec.Command(ffprobePath,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=channels",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe channels failed: %v", err)
	}
	channels, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("parse channels %q: %v", strings.TrimSpace(string(output)), err)
	}

	return channels
}

// decodeAudioSyncPCM decodes the first audio stream to normalized mono samples
// at 48 kHz so the silence and tone windows can be measured in Go.
func decodeAudioSyncPCM(t *testing.T, ffmpegPath, path string) []float64 {
	t.Helper()

	output, err := exec.Command(ffmpegPath,
		"-v", "error",
		"-i", path,
		"-map", "0:a:0",
		"-ac", "1",
		"-ar", strconv.Itoa(mixIntegrationSampleRate),
		"-f", "s16le",
		"-",
	).Output()
	if err != nil {
		t.Fatalf("decode pcm failed: %v", err)
	}
	if len(output)%2 != 0 {
		t.Fatalf("unexpected odd PCM length %d", len(output))
	}

	samples := make([]float64, len(output)/2)
	for index := range samples {
		raw := int16(binary.LittleEndian.Uint16(output[index*2 : index*2+2]))
		samples[index] = float64(raw) / 32768
	}

	return samples
}

func audioSyncWindow(samples []float64, startSeconds, endSeconds float64) []float64 {
	start := int(startSeconds * mixIntegrationSampleRate)
	end := int(endSeconds * mixIntegrationSampleRate)
	if start < 0 {
		start = 0
	}
	if end > len(samples) {
		end = len(samples)
	}
	if start >= end {
		return nil
	}

	return samples[start:end]
}

func audioSyncRMS(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, sample := range samples {
		sum += sample * sample
	}

	return math.Sqrt(sum / float64(len(samples)))
}

// audioSyncToneEnergy is a Goertzel magnitude at frequency. It is enough to
// tell a 440 Hz original apart from an 880 Hz source.
func audioSyncToneEnergy(samples []float64, frequency float64) float64 {
	if len(samples) == 0 {
		return 0
	}

	coefficient := 2 * math.Cos(2*math.Pi*frequency/mixIntegrationSampleRate)
	var previous, previousPrevious float64
	for _, sample := range samples {
		current := sample + coefficient*previous - previousPrevious
		previousPrevious = previous
		previous = current
	}
	power := previous*previous + previousPrevious*previousPrevious - coefficient*previous*previousPrevious
	if power < 0 {
		power = 0
	}

	return math.Sqrt(power) / float64(len(samples))
}

func assertAudioSyncSilence(t *testing.T, samples []float64, startSeconds, endSeconds float64) {
	t.Helper()

	if rms := audioSyncRMS(audioSyncWindow(samples, startSeconds, endSeconds)); rms > 0.005 {
		t.Fatalf("expected silence in %.2f-%.2f s, got RMS %.4f", startSeconds, endSeconds, rms)
	}
}

func assertAudioSyncTone(t *testing.T, samples []float64, startSeconds, endSeconds float64, frequency float64, minimumRMS float64) float64 {
	t.Helper()

	window := audioSyncWindow(samples, startSeconds, endSeconds)
	if rms := audioSyncRMS(window); rms < minimumRMS {
		t.Fatalf("expected a tone in %.2f-%.2f s, got RMS %.4f", startSeconds, endSeconds, rms)
	}

	return audioSyncToneEnergy(window, frequency)
}

func TestAudioSyncPositiveOffsetLeavesHeadSilent(t *testing.T) {
	ffmpegPath, ffprobePath := requireFFmpeg(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "camera.mp4")
	source := filepath.Join(dir, "recorder.m4a")
	output := filepath.Join(dir, "camera_synced.mp4")
	generateAudioSyncVideo(t, ffmpegPath, video, 4, 440)
	generateAudioSyncSource(t, ffmpegPath, source, 3, 880)

	mixErr := ExecuteMix(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, MixRequest{
		VideoPath:        video,
		OutputPath:       output,
		Mode:             MixModeReplace,
		SourcePath:       source,
		OffsetSeconds:    2,
		OriginalAudio:    MixOriginalRemove,
		SourceGainDB:     0,
		AudioBitrateKbps: 128,
		DurationSeconds:  4,
	}, nil)
	if mixErr != nil {
		t.Fatalf("execute mix: %v", mixErr)
	}

	videoCodec, videoCount, audioCount, duration := probeAudioSyncOutput(t, ffprobePath, output)
	if videoCount != 1 || audioCount != 1 {
		t.Fatalf("output streams = %d video / %d audio, want 1/1", videoCount, audioCount)
	}
	if videoCodec != "h264" {
		t.Fatalf("video codec = %q, want the copied h264", videoCodec)
	}
	if channels := probeAudioSyncChannels(t, ffprobePath, output); channels != 2 {
		t.Fatalf("output audio channels = %d, want a stereo track even from a mono source", channels)
	}
	if math.Abs(duration-4) > 0.1 {
		t.Fatalf("output duration = %.3f s, want 4 s within 100 ms", duration)
	}

	samples := decodeAudioSyncPCM(t, ffmpegPath, output)
	assertAudioSyncSilence(t, samples, 0.1, 1.8)
	sourceEnergy := assertAudioSyncTone(t, samples, 2.3, 3.8, 880, 0.02)
	originalEnergy := audioSyncToneEnergy(audioSyncWindow(samples, 2.3, 3.8), 440)
	if sourceEnergy < originalEnergy*3 {
		t.Fatalf("the source tone must dominate after 2 s: 880 Hz %.4f vs 440 Hz %.4f", sourceEnergy, originalEnergy)
	}
	t.Logf("duration=%.3f s, silence RMS=%.5f, 880 Hz energy=%.4f, 440 Hz energy=%.4f",
		duration,
		audioSyncRMS(audioSyncWindow(samples, 0.1, 1.8)),
		sourceEnergy,
		originalEnergy,
	)
}

func TestAudioSyncNegativeOffsetKeepsTheSourceHead(t *testing.T) {
	ffmpegPath, ffprobePath := requireFFmpeg(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "camera.mp4")
	source := filepath.Join(dir, "recorder.m4a")
	output := filepath.Join(dir, "camera_synced.mp4")
	generateAudioSyncVideo(t, ffmpegPath, video, 4, 440)
	generateAudioSyncSource(t, ffmpegPath, source, 3, 880)

	mixErr := ExecuteMix(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, MixRequest{
		VideoPath:        video,
		OutputPath:       output,
		Mode:             MixModeReplace,
		SourcePath:       source,
		OffsetSeconds:    -1,
		OriginalAudio:    MixOriginalRemove,
		AudioBitrateKbps: 128,
		DurationSeconds:  4,
	}, nil)
	if mixErr != nil {
		t.Fatalf("execute mix: %v", mixErr)
	}

	_, _, audioCount, duration := probeAudioSyncOutput(t, ffprobePath, output)
	if audioCount != 1 {
		t.Fatalf("output audio streams = %d, want 1", audioCount)
	}
	if math.Abs(duration-4) > 0.1 {
		t.Fatalf("output duration = %.3f s, want 4 s within 100 ms", duration)
	}

	samples := decodeAudioSyncPCM(t, ffmpegPath, output)
	energy := assertAudioSyncTone(t, samples, 0.1, 0.5, 880, 0.02)
	t.Logf("duration=%.3f s, first-window 880 Hz energy=%.4f (no leading silence)", duration, energy)
}

func TestAudioSyncSectionsLeaveGapsSilent(t *testing.T) {
	ffmpegPath, ffprobePath := requireFFmpeg(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "camera.mp4")
	source := filepath.Join(dir, "recorder.m4a")
	output := filepath.Join(dir, "camera_synced.mp4")
	generateAudioSyncVideo(t, ffmpegPath, video, 4, 440)
	generateAudioSyncSource(t, ffmpegPath, source, 3, 880)

	mixErr := ExecuteMix(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, MixRequest{
		VideoPath:        video,
		OutputPath:       output,
		Mode:             MixModeSections,
		OriginalAudio:    MixOriginalRemove,
		AudioBitrateKbps: 128,
		DurationSeconds:  4,
		Segments: []MixSegment{
			{VideoStart: 1, VideoEnd: 3, SourceStart: 0, SourcePath: source},
		},
	}, nil)
	if mixErr != nil {
		t.Fatalf("execute mix: %v", mixErr)
	}

	_, _, audioCount, duration := probeAudioSyncOutput(t, ffprobePath, output)
	if audioCount != 1 {
		t.Fatalf("output audio streams = %d, want 1", audioCount)
	}
	if math.Abs(duration-4) > 0.1 {
		t.Fatalf("output duration = %.3f s, want 4 s within 100 ms", duration)
	}

	samples := decodeAudioSyncPCM(t, ffmpegPath, output)
	assertAudioSyncSilence(t, samples, 0.1, 0.8)
	assertAudioSyncTone(t, samples, 1.3, 2.8, 880, 0.02)
	assertAudioSyncSilence(t, samples, 3.3, 3.8)
	t.Logf("duration=%.3f s, gaps silent, segment tone present", duration)
}

func TestAudioSyncNormalizeProducesOutput(t *testing.T) {
	ffmpegPath, ffprobePath := requireFFmpeg(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "camera.mp4")
	source := filepath.Join(dir, "recorder.m4a")
	output := filepath.Join(dir, "camera_synced.mp4")
	generateAudioSyncVideo(t, ffmpegPath, video, 4, 440)
	generateAudioSyncSource(t, ffmpegPath, source, 4, 880)

	mixErr := ExecuteMix(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, MixRequest{
		VideoPath:        video,
		OutputPath:       output,
		Mode:             MixModeReplace,
		SourcePath:       source,
		OriginalAudio:    MixOriginalRemove,
		AudioBitrateKbps: 128,
		Normalize:        true,
		DurationSeconds:  4,
	}, nil)
	if mixErr != nil {
		t.Fatalf("normalized mix: %v", mixErr)
	}

	_, videoCount, audioCount, duration := probeAudioSyncOutput(t, ffprobePath, output)
	if videoCount != 1 || audioCount != 1 {
		t.Fatalf("output streams = %d video / %d audio, want 1/1", videoCount, audioCount)
	}
	if math.Abs(duration-4) > 0.1 {
		t.Fatalf("output duration = %.3f s, want 4 s within 100 ms", duration)
	}

	samples := decodeAudioSyncPCM(t, ffmpegPath, output)
	energy := assertAudioSyncTone(t, samples, 0.2, 3.8, 880, 0.01)
	t.Logf("duration=%.3f s, normalized 880 Hz energy=%.4f", duration, energy)
}

func TestAudioSyncDuckKeepsBothTracks(t *testing.T) {
	ffmpegPath, ffprobePath := requireFFmpeg(t)
	dir := t.TempDir()
	video := filepath.Join(dir, "camera.mp4")
	source := filepath.Join(dir, "recorder.m4a")
	output := filepath.Join(dir, "camera_synced.mp4")
	generateAudioSyncVideo(t, ffmpegPath, video, 4, 440)
	generateAudioSyncSource(t, ffmpegPath, source, 4, 880)

	mixErr := ExecuteMix(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, MixRequest{
		VideoPath:        video,
		OutputPath:       output,
		Mode:             MixModeReplace,
		SourcePath:       source,
		OffsetSeconds:    0,
		OriginalAudio:    MixOriginalDuck,
		OriginalGainDB:   -18,
		AudioBitrateKbps: 128,
		DurationSeconds:  4,
	}, nil)
	if mixErr != nil {
		t.Fatalf("execute mix: %v", mixErr)
	}

	_, _, audioCount, duration := probeAudioSyncOutput(t, ffprobePath, output)
	if audioCount != 1 {
		t.Fatalf("output audio streams = %d, want 1", audioCount)
	}
	if math.Abs(duration-4) > 0.1 {
		t.Fatalf("output duration = %.3f s, want 4 s within 100 ms", duration)
	}

	samples := decodeAudioSyncPCM(t, ffmpegPath, output)
	window := audioSyncWindow(samples, 0.5, 3.5)
	sourceEnergy := audioSyncToneEnergy(window, 880)
	originalEnergy := audioSyncToneEnergy(window, 440)
	if sourceEnergy < 0.01 || originalEnergy < 0.002 {
		t.Fatalf("duck must keep both tracks: 880 Hz %.4f, 440 Hz %.4f", sourceEnergy, originalEnergy)
	}
	if sourceEnergy <= originalEnergy {
		t.Fatalf("the source must stay louder than the ducked original: 880 Hz %.4f vs 440 Hz %.4f", sourceEnergy, originalEnergy)
	}
	t.Logf("duration=%.3f s, 880 Hz energy=%.4f, ducked 440 Hz energy=%.4f", duration, sourceEnergy, originalEnergy)
}
