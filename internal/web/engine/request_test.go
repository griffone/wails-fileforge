package engine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestValidateRequest(t *testing.T) {
	baseDir := t.TempDir()
	missingDir := filepath.Join(baseDir, "missing")
	filePath := filepath.Join(baseDir, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	validURL := "https://www.youtube.com/watch?v=abc"

	tests := []struct {
		name       string
		req        Request
		outputDir  string
		wantCode   string
		wantDetail string
	}{
		{
			name: "video request is valid",
			req:  Request{URL: validURL, Kind: KindVideo},
		},
		{
			name: "audio request is valid",
			req:  Request{URL: validURL, Kind: KindAudio, AudioBitrateKbps: 192},
		},
		{
			name:       "empty url",
			req:        Request{URL: "", Kind: KindVideo},
			wantCode:   CodeInvalidURL,
			wantDetail: detailURLEmpty,
		},
		{
			name:       "non http scheme",
			req:        Request{URL: "ftp://example.com/clip", Kind: KindVideo},
			wantCode:   CodeInvalidURL,
			wantDetail: detailURLScheme,
		},
		{
			name:       "missing host",
			req:        Request{URL: "https://", Kind: KindVideo},
			wantCode:   CodeInvalidURL,
			wantDetail: detailURLHost,
		},
		{
			name:       "unparseable url",
			req:        Request{URL: "https://exa mple.com/clip", Kind: KindVideo},
			wantCode:   CodeInvalidURL,
			wantDetail: detailURLInvalid,
		},
		{
			name:       "missing output dir",
			req:        Request{URL: validURL, Kind: KindVideo},
			outputDir:  missingDir,
			wantCode:   CodeExecutionFailed,
			wantDetail: detailOutputDirNotFound,
		},
		{
			name:       "output path is a file",
			req:        Request{URL: validURL, Kind: KindVideo},
			outputDir:  filePath,
			wantCode:   CodeExecutionFailed,
			wantDetail: detailOutputDirNotDir,
		},
		{
			name:       "empty output dir",
			req:        Request{URL: validURL, Kind: KindVideo, OutputDir: "  "},
			wantCode:   CodeExecutionFailed,
			wantDetail: detailOutputDirRequired,
		},
		{
			name:       "invalid kind",
			req:        Request{URL: validURL, Kind: "playlist"},
			wantCode:   CodeExecutionFailed,
			wantDetail: detailKindInvalid,
		},
		{
			name:       "negative max height",
			req:        Request{URL: validURL, Kind: KindVideo, MaxHeight: -1},
			wantCode:   CodeExecutionFailed,
			wantDetail: detailMaxHeightInvalid,
		},
		{
			name:       "negative audio bitrate",
			req:        Request{URL: validURL, Kind: KindAudio, AudioBitrateKbps: -1},
			wantCode:   CodeExecutionFailed,
			wantDetail: detailBitrateInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.req
			if req.OutputDir == "" && tt.outputDir == "" {
				req.OutputDir = baseDir
			}
			if tt.outputDir != "" {
				req.OutputDir = tt.outputDir
			}

			webErr := ValidateRequest(req)
			if tt.wantCode == "" {
				if webErr != nil {
					t.Fatalf("expected the request to be valid, got %v", webErr)
				}
				return
			}

			if webErr == nil {
				t.Fatalf("expected %s, got nil", tt.wantCode)
			}
			if webErr.Code != tt.wantCode || webErr.DetailCode != tt.wantDetail {
				t.Fatalf("expected %s/%s, got %s/%s (%v)", tt.wantCode, tt.wantDetail, webErr.Code, webErr.DetailCode, webErr)
			}
		})
	}

	if _, statErr := os.Stat(missingDir); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("validation must not create the output directory, got %v", statErr)
	}
}

func TestBuildArgs(t *testing.T) {
	dir := t.TempDir()
	url := "https://www.youtube.com/watch?v=abc"

	videoHead := []string{"-f", "bv*+ba/b", "--merge-output-format", "mp4"}
	cappedHead := []string{"-f", "bv*[height<=1080]+ba/b[height<=1080]", "--merge-output-format", "mp4"}
	audioHead := func(quality string) []string {
		return []string{"-f", "ba", "-x", "--audio-format", "mp3", "--audio-quality", quality}
	}
	explicitPath := filepath.Join(dir, "planned", "Clip [abc].mp4")

	tests := []struct {
		name string
		req  Request
		want []string
	}{
		{
			name: "video best available",
			req:  Request{URL: url, OutputDir: dir, Kind: KindVideo},
			want: append(videoHead, wantDownloadTail(dir, url)...),
		},
		{
			name: "video height cap",
			req:  Request{URL: url, OutputDir: dir, Kind: KindVideo, MaxHeight: 1080},
			want: append(cappedHead, wantDownloadTail(dir, url)...),
		},
		{
			name: "audio chosen bitrate",
			req:  Request{URL: url, OutputDir: dir, Kind: KindAudio, AudioBitrateKbps: 320},
			want: append(audioHead("320K"), wantDownloadTail(dir, url)...),
		},
		{
			name: "audio best vbr",
			req:  Request{URL: url, OutputDir: dir, Kind: KindAudio},
			want: append(audioHead("0"), wantDownloadTail(dir, url)...),
		},
		{
			name: "explicit output path replaces the directory template",
			req:  Request{URL: url, OutputDir: dir, OutputPath: explicitPath, Kind: KindVideo},
			want: append(videoHead, wantExplicitDownloadTail(explicitPath, url)...),
		},
		{
			name: "explicit audio output path",
			req:  Request{URL: url, OutputDir: dir, OutputPath: filepath.Join(dir, "Clip.mp3"), Kind: KindAudio, AudioBitrateKbps: 192},
			want: append(audioHead("192K"), wantExplicitDownloadTail(filepath.Join(dir, "Clip.mp3"), url)...),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, buildErr := BuildArgs(tt.req)
			if buildErr != nil {
				t.Fatalf("expected args, got %v", buildErr)
			}
			if !reflect.DeepEqual(args, tt.want) {
				t.Fatalf("unexpected args:\n got %v\nwant %v", args, tt.want)
			}
		})
	}
}

func TestBuildArgsRejectsInvalidRequest(t *testing.T) {
	args, buildErr := BuildArgs(Request{URL: "not-a-url", OutputDir: t.TempDir(), Kind: KindVideo})
	if buildErr == nil {
		t.Fatal("expected a validation error")
	}
	if buildErr.Code != CodeInvalidURL {
		t.Fatalf("expected %s, got %s", CodeInvalidURL, buildErr.Code)
	}
	if args != nil {
		t.Fatalf("expected no args, got %v", args)
	}
}

// wantDownloadTail pins the shared half of every templated command, including
// the exact progress and output templates the parser depends on.
func wantDownloadTail(dir, url string) []string {
	return []string{
		"--no-playlist",
		"--newline",
		"--progress",
		"--progress-template", "download:ff-dl %(progress.downloaded_bytes)s %(progress.total_bytes_estimate)s %(progress.speed)s %(progress.eta)s",
		"-P", dir,
		"-o", "%(title).120B [%(id)s].%(ext)s",
		"--print", "after_move:filepath",
		"--no-simulate",
		url,
	}
}

// wantExplicitDownloadTail pins the exact-file command: no -P and no title
// template, so yt-dlp writes the path Plan resolved.
func wantExplicitDownloadTail(outputPath, url string) []string {
	return []string{
		"--no-playlist",
		"--newline",
		"--progress",
		"--progress-template", "download:ff-dl %(progress.downloaded_bytes)s %(progress.total_bytes_estimate)s %(progress.speed)s %(progress.eta)s",
		"-o", outputPath,
		"--print", "after_move:filepath",
		"--no-simulate",
		url,
	}
}
