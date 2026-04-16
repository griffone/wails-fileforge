package preview

import (
	"context"
	"testing"
)

func TestMockProcessor(t *testing.T) {
	data := []byte{0x1, 0x2, 0x3}
	p := NewMockProcessor(data, "image/webp")
	out, ct, err := p.Process(context.Background(), PreviewRequest{Path: "/tmp/a.png", Width: 16, Height: 16, Format: "auto"})
	if err != nil {
		t.Fatalf("mock process error: %v", err)
	}
	if string(out) != string(data) || ct != "image/webp" {
		t.Fatalf("unexpected result: %v %s", out, ct)
	}
}
