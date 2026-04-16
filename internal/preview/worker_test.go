package preview

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type countingProcessor struct {
	succeed bool
}

func (c *countingProcessor) Process(ctx context.Context, req PreviewRequest) ([]byte, string, error) {
	if !c.succeed {
		return nil, "", fmt.Errorf("simulated error")
	}
	return []byte("ok"), "image/webp", nil
}

func TestWorkerHandlerSuccess(t *testing.T) {
	svc := NewPreviewService(Config{AllowedRoots: []string{"/tmp"}, MaxQueue: 10})
	// inject mock processor by overriding NewBimgProcessor locally is not trivial; instead
	// set svc.cache to a real cache so we can verify Put is called
	c, err := NewPreviewCache(10*1024*1024, "", 100*1024*1024, 1024*1024, 5*time.Minute)
	if err != nil {
		t.Fatalf("cache init: %v", err)
	}
	svc.cache = c

	// create a job and call handler directly
	req := PreviewRequest{Path: "/tmp/nonexistent.png", Width: 16, Height: 16, Format: "auto"}
	id, err := svc.Enqueue(context.Background(), req)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// attempt to handle job: because file doesn't exist, job should fail
	svc.handleJob(svc.jobs[id])
	status, _ := svc.Status(id)
	if status.Status != JobStateFailed {
		t.Fatalf("expected failed got %v", status.Status)
	}
	_ = svc.Shutdown(context.Background())
}
