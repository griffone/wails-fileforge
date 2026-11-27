package app

import (
	"context"
	"errors"
	"testing"

	"fileforge-desktop/internal/platform/execx"
	"fileforge-desktop/internal/web/engine"
)

// fakeRuntimeProbe reports the configured runtime state without touching PATH.
type fakeRuntimeProbe struct {
	err error
}

func (p *fakeRuntimeProbe) Check(context.Context) error {
	return p.err
}

// fakeCommandRunner scripts one yt-dlp invocation without a process. It records
// nothing because the service test only cares about the mapped response.
type fakeCommandRunner struct {
	stdout string
	stderr string
	err    error
	calls  int
}

func (r *fakeCommandRunner) Run(_ context.Context, _ string, _ []string, emit func(line string)) (execx.Result, error) {
	r.calls++

	if emit != nil && r.stdout != "" {
		emit(r.stdout)
	}

	return execx.Result{ExitCode: 1, Stderr: r.stderr}, r.err
}

const probeFixtureJSON = `{
  "id": "abc123",
  "title": "Fixture clip",
  "uploader": "Fixture uploader",
  "thumbnail": "https://example.test/thumb.jpg",
  "duration": 92.5,
  "formats": [
    {"format_id": "137", "ext": "mp4", "vcodec": "avc1.640028", "acodec": "none", "height": 1080, "fps": 30, "filesize": 320000000, "protocol": "https"},
    {"format_id": "136", "ext": "mp4", "vcodec": "avc1.4d401f", "acodec": "none", "height": 720, "fps": 30, "filesize": 150000000, "protocol": "https"},
    {"format_id": "140", "ext": "m4a", "vcodec": "none", "acodec": "mp4a.40.2", "abr": 128, "filesize": 1400000, "protocol": "https"}
  ]
}`

func TestMediaServiceProbeLinkMapsVideoAndAudioFormats(t *testing.T) {
	service := NewMediaServiceWithDeps(
		&fakeRuntimeProbe{},
		&fakeCommandRunner{stdout: probeFixtureJSON},
	)

	response := service.ProbeLink("https://example.test/watch?v=abc123")

	if !response.Success {
		t.Fatalf("expected success, got message %q", response.Message)
	}
	if response.URL != "https://example.test/watch?v=abc123" {
		t.Fatalf("url = %q", response.URL)
	}
	if response.ID != "abc123" || response.Title != "Fixture clip" || response.Uploader != "Fixture uploader" {
		t.Fatalf("metadata not mapped: %+v", response)
	}
	if response.ThumbnailURL != "https://example.test/thumb.jpg" || response.DurationSeconds != 92.5 {
		t.Fatalf("thumbnail/duration not mapped: %+v", response)
	}
	if response.IsPlaylist {
		t.Fatalf("single video reported as playlist")
	}
	if len(response.Video) != 2 {
		t.Fatalf("expected 2 video heights, got %d", len(response.Video))
	}
	// Highest height first, with the size the probe measured plus the best
	// audio track the download policy merges in.
	if response.Video[0].Height != 1080 || response.Video[0].Ext != "mp4" || response.Video[0].SizeBytes != 321400000 {
		t.Fatalf("unexpected first video option: %+v", response.Video[0])
	}
	if response.Video[0].FPS != 30 || response.Video[0].VCodec != "avc1.640028" {
		t.Fatalf("fps/codec not mapped: %+v", response.Video[0])
	}
	if len(response.Audio) != 1 || response.Audio[0].BitrateKbps != 128 || response.Audio[0].Ext != "m4a" {
		t.Fatalf("audio options not mapped: %+v", response.Audio)
	}
	if len(response.Entries) != 0 {
		t.Fatalf("single video should have no playlist entries: %+v", response.Entries)
	}
}

func TestMediaServiceProbeLinkMapsPlaylistEntries(t *testing.T) {
	const playlistJSON = `{
	  "_type": "playlist",
	  "id": "PL1",
	  "title": "Fixture playlist",
	  "entries": [
	    {"url": "https://example.test/watch?v=one", "title": "One", "duration": 10},
	    {"url": "https://example.test/watch?v=two", "title": "Two", "duration": 20}
	  ]
	}`

	service := NewMediaServiceWithDeps(
		&fakeRuntimeProbe{},
		&fakeCommandRunner{stdout: playlistJSON},
	)

	response := service.ProbeLink("https://example.test/playlist?list=PL1")

	if !response.Success || !response.IsPlaylist {
		t.Fatalf("expected a successful playlist, got %+v", response)
	}
	if len(response.Entries) != 2 || response.Entries[0].URL != "https://example.test/watch?v=one" {
		t.Fatalf("playlist entries not mapped: %+v", response.Entries)
	}
	if response.Entries[1].Title != "Two" || response.Entries[1].DurationSeconds != 20 {
		t.Fatalf("playlist entry detail not mapped: %+v", response.Entries[1])
	}
	if len(response.Video) != 0 || len(response.Audio) != 0 {
		t.Fatalf("playlist should not carry formats: %+v", response)
	}
}

func TestMediaServiceProbeLinkReportsMissingRuntime(t *testing.T) {
	runtimeErr := &engine.WebError{
		Code:    engine.CodeRuntimeUnavailable,
		Message: "yt-dlp is not available; install it from the toolchain panel",
	}
	service := NewMediaServiceWithDeps(&fakeRuntimeProbe{err: runtimeErr}, &fakeCommandRunner{})

	response := service.ProbeLink("https://example.test/watch?v=abc")

	if response.Success {
		t.Fatalf("expected failure, got %+v", response)
	}
	if response.Message != runtimeErr.Message {
		t.Fatalf("message = %q, want %q", response.Message, runtimeErr.Message)
	}
	if response.Video == nil || response.Audio == nil || response.Entries == nil {
		t.Fatalf("lists must be initialised on failure: %+v", response)
	}
}

func TestMediaServiceProbeLinkReportsUnsupportedSite(t *testing.T) {
	service := NewMediaServiceWithDeps(
		&fakeRuntimeProbe{},
		&fakeCommandRunner{
			stderr: "ERROR: Unsupported URL: https://example.test/watch",
			err:    errors.New("exit status 1"),
		},
	)

	response := service.ProbeLink("https://example.test/watch")

	if response.Success {
		t.Fatalf("expected failure, got %+v", response)
	}
	if response.Message != "yt-dlp does not support this site or link" {
		t.Fatalf("message = %q", response.Message)
	}
}

func TestMediaServiceProbeLinkReportsNetworkFailure(t *testing.T) {
	service := NewMediaServiceWithDeps(
		&fakeRuntimeProbe{},
		&fakeCommandRunner{
			stderr: "ERROR: Unable to download webpage: <urlopen error [Errno -3] Temporary failure in name resolution>",
			err:    errors.New("exit status 1"),
		},
	)

	response := service.ProbeLink("https://example.test/watch")

	if response.Success {
		t.Fatalf("expected failure, got %+v", response)
	}
	if response.Message != "yt-dlp could not read the link metadata" {
		t.Fatalf("message = %q", response.Message)
	}
}

func TestMediaServiceProbeLinkReportsInvalidURLWithoutRunning(t *testing.T) {
	runner := &fakeCommandRunner{}
	service := NewMediaServiceWithDeps(&fakeRuntimeProbe{}, runner)

	response := service.ProbeLink("ftp://example.test/clip")

	if response.Success {
		t.Fatalf("expected failure, got %+v", response)
	}
	if response.Message == "" {
		t.Fatalf("expected a user-facing message")
	}
	if runner.calls != 0 {
		t.Fatalf("invalid URL must not run yt-dlp, calls = %d", runner.calls)
	}
}

func TestMediaServiceProbeLinkReportsUnparsableMetadata(t *testing.T) {
	service := NewMediaServiceWithDeps(
		&fakeRuntimeProbe{},
		&fakeCommandRunner{stdout: "not json"},
	)

	response := service.ProbeLink("https://example.test/watch")

	if response.Success {
		t.Fatalf("expected failure, got %+v", response)
	}
	if response.Message != "yt-dlp returned metadata that could not be parsed" {
		t.Fatalf("message = %q", response.Message)
	}
}

func TestMediaServiceProbeLinkRejectsEmptyLink(t *testing.T) {
	service := NewMediaServiceWithDeps(&fakeRuntimeProbe{}, &fakeCommandRunner{})

	response := service.ProbeLink("   ")

	if response.Success || response.Message == "" {
		t.Fatalf("expected a validation failure, got %+v", response)
	}
}

func TestMediaFailureMessageFallsBackToPlainError(t *testing.T) {
	if got := mediaFailureMessage(errors.New("boom"), "fallback"); got != "boom" {
		t.Fatalf("message = %q", got)
	}
	if got := mediaFailureMessage(nil, "fallback"); got != "fallback" {
		t.Fatalf("message = %q", got)
	}
}
