package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/media"
	"fileforge-desktop/internal/media/analysis"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/execx"
	videoEngine "fileforge-desktop/internal/video/engine"
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

// fakeMediaAnalyzer scripts the analysis seam so peaks and sync detection never
// run ffmpeg. It records the request so the service mapping is observable.
type fakeMediaAnalyzer struct {
	peaksResult   *analysis.PeaksResult
	peaksErr      *analysis.Error
	peaksRequests []analysis.PeaksRequest

	analyzeResult   *analysis.Result
	analyzeErr      *analysis.Error
	analyzeRequests []analysis.Request
}

func (f *fakeMediaAnalyzer) Peaks(_ context.Context, req analysis.PeaksRequest) (*analysis.PeaksResult, *analysis.Error) {
	f.peaksRequests = append(f.peaksRequests, req)
	if f.peaksErr != nil {
		return nil, f.peaksErr
	}
	return f.peaksResult, nil
}

func (f *fakeMediaAnalyzer) Analyze(_ context.Context, req analysis.Request) (*analysis.Result, *analysis.Error) {
	f.analyzeRequests = append(f.analyzeRequests, req)
	if f.analyzeErr != nil {
		return nil, f.analyzeErr
	}
	return f.analyzeResult, nil
}

func newAnalysisService(analyzer mediaAnalyzer) *MediaService {
	return newMediaService(mediaServiceDeps{analyzer: analyzer})
}

func TestMediaServiceAnalyzePeaksMapsWaveform(t *testing.T) {
	analyzer := &fakeMediaAnalyzer{peaksResult: &analysis.PeaksResult{
		DurationSeconds: 42,
		StartSeconds:    5,
		SpanSeconds:     10,
		HasAudio:        true,
		Min:             []float32{-0.5, -1},
		Max:             []float32{0.25, 0.75},
	}}
	service := newAnalysisService(analyzer)

	response := service.AnalyzePeaks(models.MediaPeaksRequest{
		Path:         "/tmp/input.wav",
		StartSeconds: 5,
		SpanSeconds:  10,
		Buckets:      200,
	})

	if !response.Success {
		t.Fatalf("expected success, got %q", response.Message)
	}
	if !response.HasAudio || response.DurationSeconds != 42 || response.StartSeconds != 5 || response.SpanSeconds != 10 {
		t.Fatalf("result not mapped: %+v", response)
	}
	if len(response.Min) != 2 || response.Min[0] != -0.5 || response.Max[1] != 0.75 {
		t.Fatalf("waveform not mapped: min=%v max=%v", response.Min, response.Max)
	}
	if len(analyzer.peaksRequests) != 1 {
		t.Fatalf("expected one peaks call, got %d", len(analyzer.peaksRequests))
	}
	request := analyzer.peaksRequests[0]
	if request.Path != "/tmp/input.wav" || request.Buckets != 200 || request.StartSeconds != 5 || request.SpanSeconds != 10 {
		t.Fatalf("request not forwarded: %+v", request)
	}
}

func TestMediaServiceAnalyzePeaksReportsNoAudio(t *testing.T) {
	analyzer := &fakeMediaAnalyzer{peaksResult: &analysis.PeaksResult{
		DurationSeconds: 12,
		StartSeconds:    0,
		SpanSeconds:     12,
		HasAudio:        false,
		Min:             make([]float32, 128),
		Max:             make([]float32, 128),
	}}
	service := newAnalysisService(analyzer)

	response := service.AnalyzePeaks(models.MediaPeaksRequest{Path: "/tmp/silent.mp4"})

	if !response.Success {
		t.Fatalf("no audio must stay a successful response, got %q", response.Message)
	}
	if response.HasAudio {
		t.Fatal("hasAudio = true, want false")
	}
	if len(response.Min) != 128 || len(response.Max) != 128 {
		t.Fatalf("waveform arrays must stay initialised: min=%d max=%d", len(response.Min), len(response.Max))
	}
}

func TestMediaServiceAnalyzePeaksMapsAnalysisError(t *testing.T) {
	analyzer := &fakeMediaAnalyzer{peaksErr: &analysis.Error{
		Code:    analysis.CodeDecodeFailed,
		Message: "ffmpeg could not decode the audio",
	}}
	service := newAnalysisService(analyzer)

	response := service.AnalyzePeaks(models.MediaPeaksRequest{Path: "/tmp/broken.mp4"})

	if response.Success {
		t.Fatalf("expected failure, got %+v", response)
	}
	if response.Message != "ffmpeg could not decode the audio" {
		t.Fatalf("message = %q", response.Message)
	}
	if response.Min == nil || response.Max == nil {
		t.Fatal("arrays must stay initialised on failure")
	}
}

func TestMediaServiceProbeMediaMapsStreams(t *testing.T) {
	cases := []struct {
		name      string
		metadata  inspect.VideoMetadata
		ok        bool
		wantVideo bool
		wantAudio bool
		wantFrame float64
	}{
		{
			name: "video with audio",
			metadata: inspect.VideoMetadata{
				DurationSeconds: 61.5,
				Width:           1920,
				Height:          1080,
				VideoCodec:      "h264",
				AudioCodec:      "aac",
				FrameRate:       29.97,
			},
			ok:        true,
			wantVideo: true,
			wantAudio: true,
			wantFrame: 29.97,
		},
		{
			name: "audio only",
			metadata: inspect.VideoMetadata{
				DurationSeconds: 30,
				AudioCodec:      "flac",
			},
			ok:        true,
			wantVideo: false,
			wantAudio: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := newMediaService(mediaServiceDeps{
				probeMetadata: func(context.Context, string) (inspect.VideoMetadata, bool) {
					return tc.metadata, tc.ok
				},
			})

			response := service.ProbeMedia("/tmp/input.mkv")

			if !response.Success {
				t.Fatalf("expected success, got %q", response.Message)
			}
			if response.Path != "/tmp/input.mkv" || response.DurationSeconds != tc.metadata.DurationSeconds {
				t.Fatalf("path/duration not mapped: %+v", response)
			}
			if response.HasVideo != tc.wantVideo || response.HasAudio != tc.wantAudio {
				t.Fatalf("hasVideo=%v hasAudio=%v, want %v/%v", response.HasVideo, response.HasAudio, tc.wantVideo, tc.wantAudio)
			}
			if response.FrameRate != tc.wantFrame {
				t.Fatalf("frameRate = %v, want %v", response.FrameRate, tc.wantFrame)
			}
		})
	}
}

func TestMediaServiceProbeMediaReportsUnreadableFile(t *testing.T) {
	service := newMediaService(mediaServiceDeps{
		probeMetadata: func(context.Context, string) (inspect.VideoMetadata, bool) {
			return inspect.VideoMetadata{}, false
		},
	})

	response := service.ProbeMedia("/tmp/missing.mkv")

	if response.Success || response.Message == "" {
		t.Fatalf("expected a failure with a message, got %+v", response)
	}
	if response.Path != "/tmp/missing.mkv" {
		t.Fatalf("path = %q", response.Path)
	}
}

func TestMediaServiceDetectSyncMapsWindowsAndDrift(t *testing.T) {
	drift := 96.5
	analyzer := &fakeMediaAnalyzer{analyzeResult: &analysis.Result{
		OffsetSeconds:            3.276,
		Confidence:               0.87,
		DriftPPM:                 &drift,
		ReferenceDurationSeconds: 120,
		SourceDurationSeconds:    118,
		Windows: []analysis.Window{
			{Label: "head", AnchorSeconds: 10, OffsetSeconds: 3.274, Confidence: 0.9, ResidualMs: -2},
			{Label: "tail", AnchorSeconds: 100, OffsetSeconds: 3.28, Confidence: 0.84, ResidualMs: 4},
		},
		Notes: []string{"the source clock drifts"},
	}}
	service := newAnalysisService(analyzer)

	response := service.DetectSync(models.MediaSyncRequest{
		ReferencePath:    "/tmp/video.mp4",
		SourcePath:       "/tmp/source.wav",
		MaxOffsetSeconds: 30,
		WindowSeconds:    10,
	})

	if !response.Success {
		t.Fatalf("expected success, got %q", response.Message)
	}
	if response.OffsetSeconds != 3.276 || response.Confidence != 0.87 {
		t.Fatalf("offset/confidence not mapped: %+v", response)
	}
	if response.DriftPPM == nil || *response.DriftPPM != drift {
		t.Fatalf("drift not mapped: %v", response.DriftPPM)
	}
	if len(response.Windows) != 2 || response.Windows[1].Label != "tail" || response.Windows[1].ResidualMs != 4 {
		t.Fatalf("windows not mapped: %+v", response.Windows)
	}
	if len(response.Notes) != 1 || response.Notes[0] != "the source clock drifts" {
		t.Fatalf("notes not mapped: %+v", response.Notes)
	}
	if response.ReferenceDurationSeconds != 120 || response.SourceDurationSeconds != 118 {
		t.Fatalf("durations not mapped: %+v", response)
	}

	request := analyzer.analyzeRequests[0]
	if request.ReferencePath != "/tmp/video.mp4" || request.SourcePath != "/tmp/source.wav" ||
		request.MaxOffsetSeconds != 30 || request.WindowSeconds != 10 {
		t.Fatalf("request not forwarded: %+v", request)
	}
}

func TestMediaServiceDetectSyncTreatsLowConfidenceAsSuccess(t *testing.T) {
	analyzer := &fakeMediaAnalyzer{analyzeResult: &analysis.Result{
		OffsetSeconds: 0.4,
		Confidence:    0.12,
		Windows:       []analysis.Window{},
		Notes:         []string{"confidence is low; check the sync by ear before exporting"},
	}}
	service := newAnalysisService(analyzer)

	response := service.DetectSync(models.MediaSyncRequest{
		ReferencePath: "/tmp/video.mp4",
		SourcePath:    "/tmp/unrelated.wav",
	})

	if !response.Success {
		t.Fatalf("low confidence must not be an error, got %q", response.Message)
	}
	if response.Confidence != 0.12 {
		t.Fatalf("confidence = %v", response.Confidence)
	}
	if response.DriftPPM != nil {
		t.Fatalf("drift must stay nil with no fit: %v", response.DriftPPM)
	}
	if len(response.Notes) == 0 {
		t.Fatal("expected the analyzer note on low confidence")
	}
}

func TestMediaServiceDetectSyncMapsAnalysisError(t *testing.T) {
	analyzer := &fakeMediaAnalyzer{analyzeErr: &analysis.Error{
		Code:    analysis.CodeNoAudio,
		Message: "the file has no audio stream",
	}}
	service := newAnalysisService(analyzer)

	response := service.DetectSync(models.MediaSyncRequest{
		ReferencePath: "/tmp/video.mp4",
		SourcePath:    "/tmp/silent.wav",
	})

	if response.Success {
		t.Fatalf("expected failure, got %+v", response)
	}
	if response.Message != "the file has no audio stream" {
		t.Fatalf("message = %q", response.Message)
	}
	if response.Windows == nil || response.Notes == nil {
		t.Fatal("windows/notes must stay initialised on failure")
	}
}

// previewRecorder scripts the preview renderer: it records each request, notes
// whether the previous file still existed when it was invoked, and writes a
// fake preview so media.Register has a real file to serve.
type previewRecorder struct {
	calls               []videoEngine.MixRequest
	beenCalled          bool
	previousFileExisted bool
	filesInDir          int
	err                 *videoEngine.VideoError
}

func (r *previewRecorder) render(_ context.Context, req videoEngine.MixRequest, _ float64, _ float64, outputPath string) *videoEngine.VideoError {
	if r.beenCalled {
		_, statErr := os.Stat(outputPath)
		r.previousFileExisted = statErr == nil
		entries, _ := os.ReadDir(filepath.Dir(outputPath))
		r.filesInDir = len(entries)
	}
	r.beenCalled = true
	r.calls = append(r.calls, req)
	if r.err != nil {
		return r.err
	}
	if writeErr := os.MkdirAll(filepath.Dir(outputPath), 0o755); writeErr != nil {
		return &videoEngine.VideoError{Code: videoEngine.ErrorCodeVideoAudioSyncOutputWriteFailed, Message: writeErr.Error()}
	}
	if writeErr := os.WriteFile(outputPath, []byte("preview"), 0o600); writeErr != nil {
		return &videoEngine.VideoError{Code: videoEngine.ErrorCodeVideoAudioSyncOutputWriteFailed, Message: writeErr.Error()}
	}
	return nil
}

func newPreviewService(renderer mediaPreviewRenderer) *MediaService {
	media.SetAllowedRoots(nil)
	return newMediaService(mediaServiceDeps{renderPreview: renderer})
}

func TestMediaServiceRenderSyncPreviewRegistersResult(t *testing.T) {
	recorder := &previewRecorder{}
	service := newPreviewService(recorder.render)
	t.Cleanup(func() { os.RemoveAll(service.previewDir) })

	response := service.RenderSyncPreview(models.MediaSyncPreviewRequest{
		VideoPath:        "/tmp/video.mp4",
		StartSeconds:     12,
		SpanSeconds:      6,
		Mode:             "sections",
		OriginalAudio:    "duck",
		OriginalGainDB:   -18,
		SourceGainDB:     2.5,
		AudioBitrateKbps: 256,
		DurationSeconds:  90,
		Segments: []models.MediaSyncSegment{
			{VideoStart: 12, VideoEnd: 18, SourcePath: "/tmp/source.wav", SourceStart: 4, GainDB: 1.5, FadeMs: 15},
		},
	})

	if !response.Success {
		t.Fatalf("expected success, got %q", response.Message)
	}
	if response.URL == "" {
		t.Fatal("expected a relative media URL")
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("expected one render, got %d", len(recorder.calls))
	}
	request := recorder.calls[0]
	if request.VideoPath != "/tmp/video.mp4" || request.Mode != "sections" ||
		request.OriginalAudio != "duck" || request.DurationSeconds != 90 || request.AudioBitrateKbps != 256 {
		t.Fatalf("request not mapped: %+v", request)
	}
	if len(request.Segments) != 1 {
		t.Fatalf("segments = %d", len(request.Segments))
	}
	segment := request.Segments[0]
	if segment.SourcePath != "/tmp/source.wav" || segment.SourceStart != 4 || segment.GainDB != 1.5 || segment.FadeSeconds != 0.015 {
		t.Fatalf("segment not mapped: %+v", segment)
	}
	if _, err := os.Stat(request.OutputPath); err != nil {
		t.Fatalf("expected the rendered file to exist: %v", err)
	}
}

func TestMediaServiceRenderSyncPreviewReplacesPreviousPreview(t *testing.T) {
	recorder := &previewRecorder{}
	service := newPreviewService(recorder.render)
	t.Cleanup(func() { os.RemoveAll(service.previewDir) })

	request := models.MediaSyncPreviewRequest{
		VideoPath:       "/tmp/video.mp4",
		StartSeconds:    0,
		SpanSeconds:     5,
		Mode:            "replace",
		SourcePath:      "/tmp/source.wav",
		DurationSeconds: 60,
	}

	if response := service.RenderSyncPreview(request); !response.Success {
		t.Fatalf("first preview failed: %q", response.Message)
	}
	firstPath := recorder.calls[0].OutputPath

	if response := service.RenderSyncPreview(request); !response.Success {
		t.Fatalf("second preview failed: %q", response.Message)
	}

	if len(recorder.calls) != 2 {
		t.Fatalf("expected two renders, got %d", len(recorder.calls))
	}
	if recorder.previousFileExisted {
		t.Fatal("the previous preview still existed when the new render started")
	}
	if recorder.filesInDir != 0 {
		t.Fatalf("preview directory held %d files at render start, want 0", recorder.filesInDir)
	}
	if recorder.calls[1].OutputPath != firstPath {
		t.Fatalf("preview path changed between renders: %q -> %q", firstPath, recorder.calls[1].OutputPath)
	}
}

func TestMediaServiceRenderSyncPreviewReportsRenderFailure(t *testing.T) {
	recorder := &previewRecorder{err: &videoEngine.VideoError{
		Code:    videoEngine.ErrorCodeVideoAudioSyncCancelled,
		Message: "audio sync canceled",
	}}
	service := newPreviewService(recorder.render)
	t.Cleanup(func() { os.RemoveAll(service.previewDir) })

	response := service.RenderSyncPreview(models.MediaSyncPreviewRequest{
		VideoPath:   "/tmp/video.mp4",
		SpanSeconds: 5,
		Mode:        "replace",
		SourcePath:  "/tmp/source.wav",
	})

	if response.Success {
		t.Fatalf("expected failure, got %+v", response)
	}
	if response.Message != "audio sync canceled" {
		t.Fatalf("message = %q", response.Message)
	}
	if response.URL != "" {
		t.Fatalf("url = %q, want empty on failure", response.URL)
	}
}

func TestMediaServiceRenderSyncPreviewRejectsEmptyVideo(t *testing.T) {
	recorder := &previewRecorder{}
	service := newPreviewService(recorder.render)

	response := service.RenderSyncPreview(models.MediaSyncPreviewRequest{
		VideoPath:   "   ",
		SpanSeconds: 5,
	})

	if response.Success || response.Message == "" {
		t.Fatalf("expected a validation failure, got %+v", response)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("empty video path must not render, calls = %d", len(recorder.calls))
	}
}
