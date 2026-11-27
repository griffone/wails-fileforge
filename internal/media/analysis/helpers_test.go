package analysis

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// requireFFmpeg skips a test when the host cannot run the real media runtime.
// Mirrors the helper used by the audio and video engine suites.
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

// writeWAV writes a mono 16 bit PCM WAV file with a hand-built header. The
// suite does not depend on an encoder for its fixtures.
func writeWAV(t *testing.T, path string, sampleRate int, samples []int16) {
	t.Helper()

	dataBytes := len(samples) * 2
	buffer := make([]byte, 0, 44+dataBytes)
	buffer = append(buffer, "RIFF"...)
	buffer = binary.LittleEndian.AppendUint32(buffer, uint32(36+dataBytes))
	buffer = append(buffer, "WAVE"...)
	buffer = append(buffer, "fmt "...)
	buffer = binary.LittleEndian.AppendUint32(buffer, 16)
	buffer = binary.LittleEndian.AppendUint16(buffer, 1) // PCM
	buffer = binary.LittleEndian.AppendUint16(buffer, 1) // mono
	buffer = binary.LittleEndian.AppendUint32(buffer, uint32(sampleRate))
	buffer = binary.LittleEndian.AppendUint32(buffer, uint32(sampleRate*2))
	buffer = binary.LittleEndian.AppendUint16(buffer, 2)
	buffer = binary.LittleEndian.AppendUint16(buffer, 16)
	buffer = append(buffer, "data"...)
	buffer = binary.LittleEndian.AppendUint32(buffer, uint32(dataBytes))
	for _, sample := range samples {
		buffer = binary.LittleEndian.AppendUint16(buffer, uint16(sample))
	}

	if err := os.WriteFile(path, buffer, 0o644); err != nil {
		t.Fatalf("write wav %s: %v", path, err)
	}
}

// synthSpeech builds deterministic, speech-like audio: bursts of coloured
// noise with exponential decays over a quiet noise floor. The signal is
// broadband and aperiodic, so both the RMS envelope and the PHAT correlation
// have a unique answer.
func synthSpeech(seed int64, seconds float64, sampleRate int) []int16 {
	count := int(math.Round(seconds * float64(sampleRate)))
	rng := rand.New(rand.NewSource(seed))

	amplitude := make([]float64, count)
	cursor := 0
	for cursor < count {
		gap := int((0.02 + rng.Float64()*0.18) * float64(sampleRate))
		duration := int((0.04 + rng.Float64()*0.10) * float64(sampleRate))
		level := 0.25 + rng.Float64()*0.75
		for offset := 0; offset < duration && cursor+gap+offset < count; offset++ {
			amplitude[cursor+gap+offset] = level * math.Exp(-3*float64(offset)/float64(duration))
		}
		cursor += gap + duration
	}

	samples := make([]int16, count)
	state := 0.0
	for index := 0; index < count; index++ {
		white := rng.Float64()*2 - 1
		state = 0.7*state + 0.3*white
		value := state * amplitude[index]
		value += 0.003 * (rng.Float64()*2 - 1)
		samples[index] = int16(math.Max(-1, math.Min(1, value*0.9)) * 32000)
	}

	return samples
}

// resampleLinear returns input resampled by ratio (output[j] = input[j*ratio]).
// ratio > 1 plays the content faster, which is a positive clock drift.
func resampleLinear(input []int16, ratio float64) []int16 {
	output := make([]int16, int(float64(len(input))/ratio))
	for index := range output {
		position := float64(index) * ratio
		whole := int(position)
		fraction := position - float64(whole)
		if whole+1 < len(input) {
			value := float64(input[whole])*(1-fraction) + float64(input[whole+1])*fraction
			output[index] = int16(math.Max(-32768, math.Min(32767, value)))
			continue
		}
		if whole < len(input) {
			output[index] = input[whole]
		}
	}

	return output
}

// prependSilence returns samples preceded by count silent samples.
func prependSilence(samples []int16, count int) []int16 {
	shifted := make([]int16, count+len(samples))
	copy(shifted[count:], samples)

	return shifted
}

// trimHead returns samples without their first count samples.
func trimHead(samples []int16, count int) []int16 {
	if count >= len(samples) {
		return nil
	}

	return samples[count:]
}

// fakeFailure is a scripted ffmpeg failure for one input path.
type fakeFailure struct {
	stderr string
	err    error
}

// pcmRunner fakes ffmpeg at the process boundary. It writes precomputed samples
// for the input path to the output path in the last argument, or scripts a
// failure or an oversized output for the error paths.
type pcmRunner struct {
	samples     map[string][]int16
	failures    map[string]fakeFailure
	truncate    map[string]int64
	probeStderr string
	calls       [][]string
}

func (r *pcmRunner) Run(_ context.Context, _ string, args []string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))

	// The duration probe has no output format and no output path.
	if !containsArg(args, "-f") {
		return r.probeStderr, nil
	}

	input := argAfter(args, "-i")
	if failure, ok := r.failures[input]; ok {
		return failure.stderr, failure.err
	}

	output := args[len(args)-1]
	if size, ok := r.truncate[input]; ok {
		file, err := os.Create(output)
		if err != nil {
			return "", err
		}
		if err := file.Truncate(size); err != nil {
			_ = file.Close()
			return "", err
		}
		return "", file.Close()
	}

	samples := r.samples[input]
	data := make([]byte, 0, len(samples)*bytesPerSample)
	for _, sample := range samples {
		data = binary.LittleEndian.AppendUint16(data, uint16(sample))
	}
	if err := os.WriteFile(output, data, 0o644); err != nil {
		return "", err
	}

	return "", nil
}

// decodedInputs returns the input paths of every decode call, in order.
func (r *pcmRunner) decodedInputs() []string {
	inputs := make([]string, 0, len(r.calls))
	for _, args := range r.calls {
		if containsArg(args, "-f") {
			inputs = append(inputs, argAfter(args, "-i"))
		}
	}

	return inputs
}

func containsArg(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}

	return false
}

func argAfter(args []string, flag string) string {
	for index, arg := range args {
		if arg == flag && index+1 < len(args) {
			return args[index+1]
		}
	}

	return ""
}

// assertContains fails when a note list does not carry the expected phrase.
func assertNoteContains(t *testing.T, notes []string, want string) {
	t.Helper()

	for _, note := range notes {
		if strings.Contains(strings.ToLower(note), strings.ToLower(want)) {
			return
		}
	}

	t.Fatalf("notes %v do not contain %q", notes, want)
}

// describeError keeps failure messages readable.
func describeError(err *Error) string {
	if err == nil {
		return "<nil>"
	}

	return fmt.Sprintf("%s: %s", err.Code, err.Message)
}
