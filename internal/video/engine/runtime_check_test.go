package engine

import (
	"context"
	"errors"
	"testing"
)

func TestFFmpegRuntimeProbeUnavailableVariants(t *testing.T) {
	tests := []struct {
		name     string
		lookup   func(string) (string, error)
		wantCode string
	}{
		{
			name: "ffmpeg missing",
			lookup: func(name string) (string, error) {
				if name == "ffmpeg" {
					return "", errors.New("ffmpeg missing")
				}
				return "/usr/bin/ffprobe", nil
			},
			wantCode: ErrorCodeRuntimeFFmpegNotFound,
		},
		{
			name: "ffprobe missing",
			lookup: func(name string) (string, error) {
				if name == "ffprobe" {
					return "", errors.New("ffprobe missing")
				}
				return "/usr/bin/ffmpeg", nil
			},
			wantCode: ErrorCodeRuntimeFFprobeNotFound,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			probe := &FFmpegRuntimeProbe{lookupPath: tt.lookup}

			err := probe.Check(context.Background())
			if err == nil {
				t.Fatalf("expected runtime unavailable error")
			}

			var videoErr *VideoError
			if !errors.As(err, &videoErr) {
				t.Fatalf("expected VideoError, got %T", err)
			}
			if videoErr.Code != tt.wantCode {
				t.Fatalf("expected %s, got %s", tt.wantCode, videoErr.Code)
			}
		})
	}
}

func TestFFmpegRuntimeProbeCanceled(t *testing.T) {
	probe := &FFmpegRuntimeProbe{
		lookupPath: func(string) (string, error) {
			return "/usr/bin/ok", nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := probe.Check(ctx)
	if err == nil {
		t.Fatalf("expected canceled error")
	}

	var videoErr *VideoError
	if !errors.As(err, &videoErr) {
		t.Fatalf("expected VideoError, got %T", err)
	}
	if videoErr.Code != "CANCELED" {
		t.Fatalf("expected CANCELED, got %s", videoErr.Code)
	}
}

func TestFFmpegRuntimeProbeNilReturnsUnavailable(t *testing.T) {
	var probe *FFmpegRuntimeProbe

	err := probe.Check(context.Background())
	if err == nil {
		t.Fatalf("expected runtime unavailable error")
	}

	var videoErr *VideoError
	if !errors.As(err, &videoErr) {
		t.Fatalf("expected VideoError, got %T", err)
	}
	if videoErr.Code != ErrorCodeRuntimeUnavailable {
		t.Fatalf("expected %s, got %s", ErrorCodeRuntimeUnavailable, videoErr.Code)
	}
}
