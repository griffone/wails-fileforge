package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
)

// Request describes one download job item: one link, one chosen quality, one
// output directory.
type Request struct {
	URL       string
	OutputDir string
	// Kind is KindVideo or KindAudio.
	Kind string
	// MaxHeight caps the video height; zero means best available.
	MaxHeight int
	// AudioBitrateKbps is the MP3 bitrate; zero means best VBR.
	AudioBitrateKbps int
}

// ValidateRequest checks the URL, the kind, the quality values, and that the
// output directory exists and is writable. It never creates the directory:
// creating it silently would hide a wrong path behind a successful download.
func ValidateRequest(req Request) *WebError {
	if err := validateURL(req.URL); err != nil {
		return err
	}

	if err := validateOutputDir(req.OutputDir); err != nil {
		return err
	}

	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind != KindVideo && kind != KindAudio {
		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailKindInvalid,
			Message:    "kind must be video or audio",
			Details:    map[string]any{"kind": req.Kind},
		}
	}

	if req.MaxHeight < 0 {
		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailMaxHeightInvalid,
			Message:    "maxHeight must be >= 0",
			Details:    map[string]any{"maxHeight": req.MaxHeight},
		}
	}

	if req.AudioBitrateKbps < 0 {
		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailBitrateInvalid,
			Message:    "audioBitrateKbps must be >= 0",
			Details:    map[string]any{"audioBitrateKbps": req.AudioBitrateKbps},
		}
	}

	return nil
}

// BuildArgs returns the exact yt-dlp argument slice for the request. The URL is
// last; the output directory and name template are shared by both kinds so the
// progress and after_move contracts stay identical.
func BuildArgs(req Request) ([]string, *WebError) {
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}

	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	args := make([]string, 0, 20)

	switch kind {
	case KindVideo:
		args = append(args, "-f", videoSelector(req.MaxHeight))
		args = append(args, "--merge-output-format", "mp4")
	case KindAudio:
		args = append(args,
			"-f", "ba",
			"-x",
			"--audio-format", "mp3",
			"--audio-quality", audioQuality(req.AudioBitrateKbps),
		)
	}

	args = append(args,
		"--no-playlist",
		"--newline",
		// --print implies --quiet, and quiet suppresses the progress stream.
		// --progress restores it while keeping the other log noise out of
		// stdout, so the only non-progress line is the after_move path.
		"--progress",
		"--progress-template", downloadProgressTemplate,
		"-P", strings.TrimSpace(req.OutputDir),
		"-o", outputTemplate,
		"--print", "after_move:filepath",
		"--no-simulate",
		strings.TrimSpace(req.URL),
	)

	return args, nil
}

// videoSelector is a policy, never a hardcoded format id: take the best video
// up to the cap plus the best audio, and fall back to the best single file when
// a merge is not possible.
func videoSelector(maxHeight int) string {
	if maxHeight <= 0 {
		return "bv*+ba/b"
	}

	cap := fmt.Sprintf("[height<=%d]", maxHeight)
	return "bv*" + cap + "+ba/b" + cap
}

func audioQuality(bitrateKbps int) string {
	if bitrateKbps <= 0 {
		return "0"
	}

	return fmt.Sprintf("%dK", bitrateKbps)
}

func validateURL(rawURL string) *WebError {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return &WebError{
			Code:       CodeInvalidURL,
			DetailCode: detailURLEmpty,
			Message:    "a link is required",
		}
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return &WebError{
			Code:       CodeInvalidURL,
			DetailCode: detailURLInvalid,
			Message:    "the link is not a valid URL",
			Details:    map[string]any{"url": trimmed, "error": err.Error()},
		}
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return &WebError{
			Code:       CodeInvalidURL,
			DetailCode: detailURLScheme,
			Message:    "the link must use http or https",
			Details:    map[string]any{"url": trimmed, "scheme": parsed.Scheme},
		}
	}

	if parsed.Hostname() == "" {
		return &WebError{
			Code:       CodeInvalidURL,
			DetailCode: detailURLHost,
			Message:    "the link must include a host",
			Details:    map[string]any{"url": trimmed},
		}
	}

	return nil
}

func validateOutputDir(rawDir string) *WebError {
	dir := strings.TrimSpace(rawDir)
	if dir == "" {
		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailOutputDirRequired,
			Message:    "an output directory is required",
		}
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &WebError{
				Code:       CodeExecutionFailed,
				DetailCode: detailOutputDirNotFound,
				Message:    fmt.Sprintf("the output directory does not exist: %s", dir),
				Details:    map[string]any{"outputDir": dir},
			}
		}

		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailOutputDirNotWritable,
			Message:    fmt.Sprintf("the output directory is not accessible: %s", dir),
			Details:    map[string]any{"outputDir": dir, "error": err.Error()},
		}
	}

	if !info.IsDir() {
		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailOutputDirNotDir,
			Message:    fmt.Sprintf("the output path is not a directory: %s", dir),
			Details:    map[string]any{"outputDir": dir},
		}
	}

	probe, err := os.CreateTemp(dir, ".fileforge-web-writecheck-*")
	if err != nil {
		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailOutputDirNotWritable,
			Message:    fmt.Sprintf("the output directory is not writable: %s", dir),
			Details:    map[string]any{"outputDir": dir, "error": err.Error()},
		}
	}
	probePath := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(probePath)
		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailOutputDirNotWritable,
			Message:    fmt.Sprintf("the output directory is not writable: %s", dir),
			Details:    map[string]any{"outputDir": dir, "error": closeErr.Error()},
		}
	}
	if removeErr := os.Remove(probePath); removeErr != nil {
		return &WebError{
			Code:       CodeExecutionFailed,
			DetailCode: detailOutputDirNotWritable,
			Message:    fmt.Sprintf("the output directory is not writable: %s", dir),
			Details:    map[string]any{"outputDir": dir, "error": removeErr.Error()},
		}
	}

	return nil
}
