package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildLDFlagsInjectsTheVersionIdentity(t *testing.T) {
	flags, err := buildLDFlags(buildInfo{version: "1.2.0", commit: "a1b2c3d", builtAt: "2026-11-26T12:00:00Z"}, "")
	if err != nil {
		t.Fatalf("buildLDFlags() error = %v", err)
	}

	for _, want := range []string{
		"-X fileforge-desktop/internal/version.Version=1.2.0",
		"-X fileforge-desktop/internal/version.Commit=a1b2c3d",
		"-X fileforge-desktop/internal/version.BuiltAt=2026-11-26T12:00:00Z",
	} {
		if !strings.Contains(flags, want) {
			t.Fatalf("flags %q do not contain %q", flags, want)
		}
	}
	if strings.Contains(flags, "update.PublicKey") {
		t.Fatalf("flags %q pinned a key when none was supplied", flags)
	}
}

func TestBuildLDFlagsInjectsTheUpdatePublicKey(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(public)

	flags, err := buildLDFlags(buildInfo{version: "1.2.0", commit: "a1b2c3d", builtAt: "now"}, encoded)
	if err != nil {
		t.Fatalf("buildLDFlags() error = %v", err)
	}
	want := "-X fileforge-desktop/internal/update.PublicKey=" + encoded
	if !strings.Contains(flags, want) {
		t.Fatalf("flags %q do not contain %q", flags, want)
	}
}

func TestBuildLDFlagsRejectsAMalformedUpdatePublicKey(t *testing.T) {
	if _, err := buildLDFlags(buildInfo{version: "1.2.0", commit: "a1b2c3d", builtAt: "now"}, "not base64 !!!"); err == nil {
		t.Fatal("buildLDFlags() accepted a malformed public key")
	}
}
