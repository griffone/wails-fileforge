package preview

import (
	"context"
	"testing"
)

func TestEnqueueAndStatus(t *testing.T) {
	svc := NewPreviewService(Config{AllowedRoots: []string{"/tmp"}, MaxQueue: 10})
	ctx := context.Background()

	req := PreviewRequest{Path: "/tmp/foo.png", Width: 64, Height: 64, Format: "auto"}
	id, err := svc.Enqueue(ctx, req)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	if id == "" {
		t.Fatalf("expected job id")
	}

	status, ok := svc.Status(id)
	if !ok {
		t.Fatalf("expected job present")
	}
	if status.Status != JobStateQueued {
		t.Fatalf("expected queued got %v", status.Status)
	}
}
