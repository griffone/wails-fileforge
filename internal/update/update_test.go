package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

func TestConfiguredFollowsTheInjectedValue(t *testing.T) {
	restore := setPublicKey(t, "")
	t.Cleanup(restore)

	if Configured() {
		t.Fatal("Configured() = true for an empty key")
	}
	if _, err := Key(); !errors.Is(err, ErrNoPublicKey) {
		t.Fatalf("Key() error = %v, want ErrNoPublicKey", err)
	}

	setPublicKey(t, "  "+newPublicKey(t)+"  ")
	if !Configured() {
		t.Fatal("Configured() = false for a non-empty key")
	}
	if _, err := Key(); err != nil {
		t.Fatalf("Key() error = %v", err)
	}
}

func TestParsePublicKeyAcceptsBothBase64Forms(t *testing.T) {
	encoded := newPublicKey(t)

	padded, err := ParsePublicKey(encoded)
	if err != nil {
		t.Fatalf("ParsePublicKey(padded) error = %v", err)
	}
	if len(padded) != ed25519.PublicKeySize {
		t.Fatalf("padded key has %d bytes", len(padded))
	}

	unpadded, err := ParsePublicKey(base64.RawStdEncoding.EncodeToString(padded))
	if err != nil {
		t.Fatalf("ParsePublicKey(unpadded) error = %v", err)
	}
	if string(unpadded) != string(padded) {
		t.Fatal("padded and unpadded encodings decoded to different keys")
	}
}

func TestParsePublicKeyRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "not base64", value: "this is not base64"},
		{name: "truncated", value: base64.StdEncoding.EncodeToString([]byte("short"))},
		{name: "too long", value: base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize+1))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParsePublicKey(test.value); err == nil {
				t.Fatalf("ParsePublicKey(%q) succeeded, want an error", test.value)
			}
		})
	}
}

func setPublicKey(t *testing.T, value string) func() {
	t.Helper()

	previous := PublicKey
	PublicKey = value
	return func() {
		PublicKey = previous
	}
}

func newPublicKey(t *testing.T) string {
	t.Helper()

	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(public)
}
