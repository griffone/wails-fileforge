package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	privateKeySuffix  = ".key"
	publicKeySuffix   = ".pub"
	signatureSuffix   = ".sig"
	privateKeyMode    = 0o600
	publicKeyMode     = 0o644
	signatureFileMode = 0o644
)

// generateKeyPair creates an ed25519 keypair for release signing.
//
// The private key is written to <prefix>.key with mode 0600 and the base64
// public key to <prefix>.pub. The public key is also returned so the caller
// can print it for build-time injection. Existing files are never overwritten:
// rotating a release key invalidates every signature made with the old one, so
// it must be a deliberate act rather than a side effect of rerunning a command.
func generateKeyPair(prefix string) (string, error) {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return "", errors.New("a key output prefix is required")
	}

	privatePath := trimmed + privateKeySuffix
	publicPath := trimmed + publicKeySuffix
	if err := requireAbsent(privatePath); err != nil {
		return "", err
	}
	if err := requireAbsent(publicPath); err != nil {
		return "", err
	}
	if dir := filepath.Dir(privatePath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("create key directory %q: %w", dir, err)
		}
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ed25519 keypair: %w", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return "", fmt.Errorf("encode private key: %w", err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	if err := os.WriteFile(privatePath, privatePEM, privateKeyMode); err != nil {
		return "", fmt.Errorf("write private key %q: %w", privatePath, err)
	}
	// WriteFile mode is filtered by the umask; enforce 0600 explicitly.
	if err := os.Chmod(privatePath, privateKeyMode); err != nil {
		return "", fmt.Errorf("set private key permissions on %q: %w", privatePath, err)
	}

	encodedPublic := base64.StdEncoding.EncodeToString(public)
	if err := os.WriteFile(publicPath, []byte(encodedPublic+"\n"), publicKeyMode); err != nil {
		return "", fmt.Errorf("write public key %q: %w", publicPath, err)
	}

	return encodedPublic, nil
}

// loadPrivateKey reads a PKCS#8 PEM ed25519 private key created by
// generateKeyPair.
func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key %q: %w", path, err)
	}

	block, _ := pem.Decode(payload)
	if block == nil {
		return nil, fmt.Errorf("private key %q is not PEM encoded", path)
	}
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("private key %q is %q, want PKCS#8 %q", path, block.Type, "PRIVATE KEY")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key %q: %w", path, err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key %q is %T, want ed25519", path, parsed)
	}

	return key, nil
}

// signArtifacts writes a detached <artifact>.sig for every artifact.
//
// The signature is ed25519 over the artifact's raw SHA-256 digest, exactly
// what the Wails updater computes and verifies. The file is standard base64 of
// the 64-byte signature plus a newline: no timestamps, no key id, so signing
// the same artifact twice produces byte-identical output.
func signArtifacts(key ed25519.PrivateKey, artifacts []string) ([]string, error) {
	if len(artifacts) == 0 {
		return nil, errors.New("no artifacts to sign")
	}

	// Read and check every artifact before writing any .sig so a typo in the
	// middle of a list cannot leave a half-signed directory.
	digests := make([][sha256.Size]byte, len(artifacts))
	for index, artifact := range artifacts {
		digest, err := digestFile(artifact)
		if err != nil {
			return nil, err
		}
		digests[index] = digest
	}

	signaturePaths := make([]string, len(artifacts))
	for index, artifact := range artifacts {
		signature := ed25519.Sign(key, digests[index][:])
		path := artifact + signatureSuffix
		payload := base64.StdEncoding.EncodeToString(signature) + "\n"
		if err := os.WriteFile(path, []byte(payload), signatureFileMode); err != nil {
			return nil, fmt.Errorf("write signature %q: %w", path, err)
		}
		signaturePaths[index] = path
	}

	return signaturePaths, nil
}

// verifyArtifact checks the detached signature over the artifact digest. The
// public key is base64 (standard or unpadded) of the raw 32 key bytes.
func verifyArtifact(artifact, signaturePath, publicKeyValue string) error {
	publicKey, err := parsePublicKey(publicKeyValue)
	if err != nil {
		return err
	}

	digest, err := digestFile(artifact)
	if err != nil {
		return err
	}

	signaturePayload, err := os.ReadFile(signaturePath)
	if err != nil {
		return fmt.Errorf("read signature %q: %w", signaturePath, err)
	}
	signature, err := decodeSignature(string(signaturePayload))
	if err != nil {
		return fmt.Errorf("signature %q: %w", signaturePath, err)
	}

	if !ed25519.Verify(publicKey, digest[:], signature) {
		return fmt.Errorf("signature %q does not match artifact %q", signaturePath, artifact)
	}

	return nil
}

func digestFile(path string) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte

	info, err := os.Stat(path)
	if err != nil {
		return zero, fmt.Errorf("stat artifact %q: %w", path, err)
	}
	if info.IsDir() {
		return zero, fmt.Errorf("artifact %q is a directory, not a file", path)
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		return zero, fmt.Errorf("read artifact %q: %w", path, err)
	}

	return sha256.Sum256(payload), nil
}

func parsePublicKey(value string) (ed25519.PublicKey, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("a base64 public key is required")
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(trimmed)
	}
	if err != nil {
		return nil, fmt.Errorf("public key is not valid base64: %w", err)
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key must be %d bytes, got %d", ed25519.PublicKeySize, len(decoded))
	}

	return ed25519.PublicKey(decoded), nil
}

func decodeSignature(value string) ([]byte, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("signature file is empty")
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(trimmed)
	}
	if err != nil {
		return nil, fmt.Errorf("signature is not valid base64: %w", err)
	}
	if len(decoded) != ed25519.SignatureSize {
		return nil, fmt.Errorf("signature must be %d bytes, got %d", ed25519.SignatureSize, len(decoded))
	}

	return decoded, nil
}

func requireAbsent(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("%q already exists; refusing to overwrite a release key", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat %q: %w", path, err)
	}
	return nil
}
