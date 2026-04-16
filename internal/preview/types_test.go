package preview

import (
	"encoding/json"
	"testing"
)

func TestPreviewRequestMarshalUnmarshal(t *testing.T) {
	req := PreviewRequest{Path: "/tmp/foo.png", Width: 128, Height: 128, Format: "auto"}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got PreviewRequest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Path != req.Path || got.Width != req.Width || got.Height != req.Height || got.Format != req.Format {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, req)
	}
}

func TestJobStatusMarshal(t *testing.T) {
	s := JobStatus{Status: JobStateQueued, Progress: 0, Message: "ok"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got JobStatus
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Status != s.Status || got.Progress != s.Progress || got.Message != s.Message {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, s)
	}
}
