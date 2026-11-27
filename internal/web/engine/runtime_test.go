package engine

import (
	"context"
	"errors"
	"testing"
)

func TestToolRuntimeProbeCheck(t *testing.T) {
	tests := []struct {
		name       string
		ctx        context.Context
		lookupErr  error
		wantCode   string
		wantDetail string
	}{
		{
			name: "binary present",
			ctx:  context.Background(),
		},
		{
			name:       "binary missing",
			ctx:        context.Background(),
			lookupErr:  errors.New("executable file not found in $PATH"),
			wantCode:   CodeRuntimeUnavailable,
			wantDetail: detailRuntimeNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &ToolRuntimeProbe{
				binary: toolBinary,
				lookupPath: func(string) (string, error) {
					if tt.lookupErr != nil {
						return "", tt.lookupErr
					}
					return "/usr/bin/yt-dlp", nil
				},
			}

			checkErr := probe.Check(tt.ctx)
			if tt.wantCode == "" {
				if checkErr != nil {
					t.Fatalf("expected the runtime to be available, got %v", checkErr)
				}
				return
			}

			webErr, ok := checkErr.(*WebError)
			if !ok {
				t.Fatalf("expected a *WebError, got %T", checkErr)
			}
			if webErr.Code != tt.wantCode || webErr.DetailCode != tt.wantDetail {
				t.Fatalf("expected %s/%s, got %s/%s", tt.wantCode, tt.wantDetail, webErr.Code, webErr.DetailCode)
			}
		})
	}
}

func TestToolRuntimeProbeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	probe := &ToolRuntimeProbe{binary: toolBinary, lookupPath: func(string) (string, error) {
		t.Fatal("lookup must not run with a canceled context")
		return "", nil
	}}

	checkErr := probe.Check(ctx)
	webErr, ok := checkErr.(*WebError)
	if !ok || webErr.Code != CodeCanceled {
		t.Fatalf("expected a canceled error, got %v", checkErr)
	}
}

func TestNilToolRuntimeProbe(t *testing.T) {
	var probe *ToolRuntimeProbe

	checkErr := probe.Check(context.Background())
	webErr, ok := checkErr.(*WebError)
	if !ok || webErr.Code != CodeRuntimeUnavailable || webErr.DetailCode != detailRuntimeProbeUnset {
		t.Fatalf("expected an unavailable error, got %v", checkErr)
	}
}

func TestNewToolRuntimeProbe(t *testing.T) {
	if probe := NewToolRuntimeProbe(); probe == nil {
		t.Fatal("expected a probe")
	}
}
