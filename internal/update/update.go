// Package update holds the trust anchor for signed release updates.
//
// PublicKey is the base64-encoded ed25519 public key an updater pins at build
// time with `-X fileforge-desktop/internal/update.PublicKey=...`. The build
// scripts inject it from FILEFORGE_UPDATE_PUBKEY; a plain `go build` leaves it
// empty, which disables the update feature instead of trusting whatever a
// release feed claims.
//
// The private key that produces the matching signatures never lives in this
// repository. See docs/release.md.
package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// PublicKey is the base64-encoded ed25519 public key for release signatures.
//
// It is empty by default. Release builds inject the real key with
// `-X fileforge-desktop/internal/update.PublicKey=<base64>`. An empty value
// means update checking is disabled: the app has no trust anchor, so it must
// never accept a release.
var PublicKey = ""

// ErrNoPublicKey reports that this build has no pinned update key.
var ErrNoPublicKey = errors.New("update: no public key is pinned in this build")

// Configured reports whether the build pinned an update public key. It checks
// only presence; use Key or ParsePublicKey to reject a malformed value.
func Configured() bool {
	return strings.TrimSpace(PublicKey) != ""
}

// Key decodes the pinned public key. It returns ErrNoPublicKey when no key was
// injected at build time.
func Key() (ed25519.PublicKey, error) {
	return ParsePublicKey(PublicKey)
}

// ParsePublicKey decodes a base64 ed25519 public key. It accepts standard
// base64 with or without padding and requires exactly 32 raw bytes, the format
// the release signer prints and the Wails updater expects.
func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, ErrNoPublicKey
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(trimmed)
	}
	if err != nil {
		return nil, fmt.Errorf("update: public key is not valid base64: %w", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("update: public key must be %d bytes, got %d", ed25519.PublicKeySize, len(decoded))
	}

	return ed25519.PublicKey(decoded), nil
}
