package engine

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"fileforge-desktop/internal/platform/execx"
)

func TestProbeLinkGroupsVideoFormatsByHeight(t *testing.T) {
	fixture := loadFixture(t, "video_info.json")
	runner := &fakeRunner{run: func(_ runnerCall, emit func(string)) (execx.Result, error) {
		emit(fixture)
		return execx.Result{}, nil
	}}

	info, probeErr := ProbeLink(context.Background(), runner, "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	if probeErr != nil {
		t.Fatalf("expected a probe result, got %v", probeErr)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected one metadata call, got %d", len(runner.calls))
	}
	wantArgs := []string{"-J", "--no-playlist", "--no-warnings", "https://www.youtube.com/watch?v=aqz-KE-bpKQ"}
	if !reflect.DeepEqual(runner.calls[0].args, wantArgs) {
		t.Fatalf("unexpected probe args: %v", runner.calls[0].args)
	}

	if info.ID != "aqz-KE-bpKQ" || info.Title != "Big Buck Bunny 60fps 4K - Official Blender Foundation Short Film" {
		t.Fatalf("unexpected identity: %+v", info)
	}
	if info.Uploader != "Blender" || info.ThumbnailURL == "" || info.DurationSeconds != 635 {
		t.Fatalf("unexpected metadata: %+v", info)
	}
	if info.IsPlaylist {
		t.Fatal("a video link must not be reported as a playlist")
	}

	want := []VideoOption{
		{Height: 2160, Ext: "mp4", VCodec: "av01.0.13M.08", FPS: 60, SizeBytes: 930767602, HDR: true},
		{Height: 1440, Ext: "webm", VCodec: "vp9", FPS: 60, SizeBytes: 504131231},
		{Height: 1080, Ext: "mp4", VCodec: "avc1.64002A", FPS: 60, SizeBytes: 288387199},
		{Height: 720, Ext: "mp4", VCodec: "avc1.4D4020", FPS: 60, SizeBytes: 181292447},
		{Height: 480, Ext: "mp4", VCodec: "avc1.4D401F", FPS: 30, SizeBytes: 58974695},
		{Height: 360, Ext: "mp4", VCodec: "avc1.42001E", FPS: 30, SizeBytes: 28526904},
		{Height: 240, Ext: "mp4", VCodec: "avc1.4D4015", FPS: 30, SizeBytes: 40091042},
		{Height: 144, Ext: "webm", VCodec: "vp9", FPS: 30, SizeBytes: 36057344},
	}
	if !reflect.DeepEqual(info.Video, want) {
		t.Fatalf("unexpected video options:\n got %+v\nwant %+v", info.Video, want)
	}
}

func TestProbeLinkListsBestAudioFormatPerCodecAndExt(t *testing.T) {
	fixture := loadFixture(t, "video_info.json")
	runner := &fakeRunner{run: func(_ runnerCall, emit func(string)) (execx.Result, error) {
		emit(fixture)
		return execx.Result{}, nil
	}}

	info, probeErr := ProbeLink(context.Background(), runner, "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	if probeErr != nil {
		t.Fatalf("expected a probe result, got %v", probeErr)
	}

	want := []AudioOption{
		{Ext: "m4a", ACodec: "mp4a.40.2", BitrateKbps: 388, SizeBytes: 30767602},
		{Ext: "m4a", ACodec: "ac-3", BitrateKbps: 384, SizeBytes: 30468745},
		{Ext: "m4a", ACodec: "ec-3", BitrateKbps: 384, SizeBytes: 30468745},
		{Ext: "webm", ACodec: "opus", BitrateKbps: 129, SizeBytes: 10202162},
	}
	if !reflect.DeepEqual(info.Audio, want) {
		t.Fatalf("unexpected audio options:\n got %+v\nwant %+v", info.Audio, want)
	}
}

func TestProbeLinkRefillsPlaylistsWithFlatPlaylist(t *testing.T) {
	playlistInfo := loadFixture(t, "playlist_info.json")
	playlistFlat := loadFixture(t, "playlist_flat.json")
	runner := &fakeRunner{run: func(call runnerCall, emit func(string)) (execx.Result, error) {
		if argsContain(call.args, "--flat-playlist") {
			emit(playlistFlat)
			return execx.Result{}, nil
		}

		emit(playlistInfo)
		return execx.Result{}, nil
	}}

	info, probeErr := ProbeLink(context.Background(), runner, "https://www.youtube.com/playlist?list=PLFIXTURE")
	if probeErr != nil {
		t.Fatalf("expected a probe result, got %v", probeErr)
	}

	if len(runner.calls) != 2 {
		t.Fatalf("expected a normal pass and a flat pass, got %d calls", len(runner.calls))
	}
	if argsContain(runner.calls[0].args, "--flat-playlist") {
		t.Fatalf("first pass must not be flat: %v", runner.calls[0].args)
	}
	if !argsContain(runner.calls[1].args, "--flat-playlist") {
		t.Fatalf("second pass must add --flat-playlist: %v", runner.calls[1].args)
	}

	if !info.IsPlaylist {
		t.Fatal("expected a playlist result")
	}
	if info.ID != "PLFIXTURE" || info.Title != "Fixture playlist" || info.Uploader != "Fixture channel" {
		t.Fatalf("unexpected playlist metadata: %+v", info)
	}
	if len(info.Video) != 0 || len(info.Audio) != 0 {
		t.Fatalf("a playlist must not expose per-link formats: %+v", info)
	}

	wantEntries := []LinkEntry{
		{URL: "https://www.youtube.com/watch?v=aaa", Title: "First item", DurationSeconds: 12.5},
		{URL: "https://www.youtube.com/watch?v=bbb", Title: "Second item", DurationSeconds: 300},
		{URL: "https://www.youtube.com/watch?v=ccc", Title: "Third item", DurationSeconds: 0},
	}
	if !reflect.DeepEqual(info.Entries, wantEntries) {
		t.Fatalf("unexpected entries:\n got %+v\nwant %+v", info.Entries, wantEntries)
	}
}

func TestProbeLinkClassifiesFailures(t *testing.T) {
	tests := []struct {
		name            string
		result          execx.Result
		err             error
		stdout          string
		wantCode        string
		wantDetail      string
		wantMessagePart string
	}{
		{
			name:            "missing runtime",
			result:          execx.Result{ExitCode: -1},
			err:             &execx.Error{ExitCode: -1, Err: exec.ErrNotFound},
			wantCode:        CodeRuntimeUnavailable,
			wantDetail:      detailRuntimeNotFound,
			wantMessagePart: "not available",
		},
		{
			name:            "unsupported site",
			result:          execx.Result{ExitCode: 1, Stderr: "ERROR: Unsupported URL: https://example.com/clip"},
			err:             errors.New("exit status 1"),
			wantCode:        CodeUnsupportedSite,
			wantDetail:      detailUnsupportedURL,
			wantMessagePart: "does not support",
		},
		{
			name:            "missing javascript runtime",
			result:          execx.Result{ExitCode: 1, Stderr: "WARNING: [youtube] No supported JavaScript runtime could be found. Only deno is enabled by default"},
			err:             errors.New("exit status 1"),
			wantCode:        CodeJSRuntimeMissing,
			wantDetail:      detailJSRuntimeMissing,
			wantMessagePart: "Deno",
		},
		{
			name:            "generic metadata failure",
			result:          execx.Result{ExitCode: 2, Stderr: "ERROR: [youtube] aqz: Video unavailable"},
			err:             errors.New("exit status 2"),
			wantCode:        CodeMetadataFailed,
			wantDetail:      detailMetadataCommandFail,
			wantMessagePart: "could not read",
		},
		{
			name:            "unparseable metadata",
			result:          execx.Result{},
			stdout:          "not json",
			wantCode:        CodeMetadataFailed,
			wantDetail:      detailMetadataInvalidJSON,
			wantMessagePart: "could not be parsed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{run: func(_ runnerCall, emit func(string)) (execx.Result, error) {
				if tt.stdout != "" {
					emit(tt.stdout)
				}
				return tt.result, tt.err
			}}

			info, probeErr := ProbeLink(context.Background(), runner, "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
			if probeErr == nil {
				t.Fatalf("expected a failure, got %+v", info)
			}
			if probeErr.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s (%v)", tt.wantCode, probeErr.Code, probeErr)
			}
			if probeErr.DetailCode != tt.wantDetail {
				t.Fatalf("expected detail %s, got %s", tt.wantDetail, probeErr.DetailCode)
			}
			if !strings.Contains(probeErr.Message, tt.wantMessagePart) {
				t.Fatalf("expected message to contain %q, got %q", tt.wantMessagePart, probeErr.Message)
			}

			if tt.name == "generic metadata failure" {
				stderr, _ := probeErr.Details["stderr"].(string)
				if !strings.Contains(stderr, "Video unavailable") {
					t.Fatalf("expected the stderr tail in Details, got %v", probeErr.Details)
				}
				if probeErr.Details["exitCode"] != 2 {
					t.Fatalf("expected exitCode 2 in Details, got %v", probeErr.Details)
				}
			}
		})
	}
}

func TestProbeLinkMapsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &fakeRunner{run: func(_ runnerCall, _ func(string)) (execx.Result, error) {
		return execx.Result{}, context.Canceled
	}}

	_, probeErr := ProbeLink(ctx, runner, "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	if probeErr == nil || probeErr.Code != CodeCanceled {
		t.Fatalf("expected a canceled error, got %v", probeErr)
	}
}

func TestProbeLinkRejectsInvalidURLBeforeRunning(t *testing.T) {
	runner := &fakeRunner{}

	_, probeErr := ProbeLink(context.Background(), runner, "ftp://example.com/clip")
	if probeErr == nil || probeErr.Code != CodeInvalidURL {
		t.Fatalf("expected an invalid URL error, got %v", probeErr)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("no command must run for an invalid URL, got %d calls", len(runner.calls))
	}
}
