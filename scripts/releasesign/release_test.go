package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeygenProducesAUsablePair(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "release")

	encodedPublic, err := generateKeyPair(prefix)
	if err != nil {
		t.Fatalf("generateKeyPair() error = %v", err)
	}

	privatePath := prefix + privateKeySuffix
	info, err := os.Stat(privatePath)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if mode := info.Mode().Perm(); mode != privateKeyMode {
		t.Fatalf("private key mode = %o, want %o", mode, privateKeyMode)
	}

	publicFile, err := os.ReadFile(prefix + publicKeySuffix)
	if err != nil {
		t.Fatalf("read public key file: %v", err)
	}
	if strings.TrimSpace(string(publicFile)) != encodedPublic {
		t.Fatalf("public key file = %q, want %q", strings.TrimSpace(string(publicFile)), encodedPublic)
	}

	privateKey, err := loadPrivateKey(privatePath)
	if err != nil {
		t.Fatalf("loadPrivateKey() error = %v", err)
	}
	derived := base64.StdEncoding.EncodeToString(privateKey.Public().(ed25519.PublicKey))
	if derived != encodedPublic {
		t.Fatalf("derived public key = %q, want %q", derived, encodedPublic)
	}
	if _, err := parsePublicKey(encodedPublic); err != nil {
		t.Fatalf("parsePublicKey(printed key) error = %v", err)
	}

	if _, err := generateKeyPair(prefix); err == nil {
		t.Fatal("generateKeyPair() overwrote an existing key")
	}
}

func TestSignThenVerifySucceedsAndIsDeterministic(t *testing.T) {
	privateKey, publicKey := testKeypair(t)
	artifact := writeArtifact(t, "artifact.bin", []byte("release payload"))

	first, err := signArtifacts(privateKey, []string{artifact})
	if err != nil {
		t.Fatalf("signArtifacts() error = %v", err)
	}
	firstPayload, err := os.ReadFile(first[0])
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}

	second, err := signArtifacts(privateKey, []string{artifact})
	if err != nil {
		t.Fatalf("second signArtifacts() error = %v", err)
	}
	secondPayload, err := os.ReadFile(second[0])
	if err != nil {
		t.Fatalf("read second signature: %v", err)
	}
	if !bytes.Equal(firstPayload, secondPayload) {
		t.Fatal("signing the same artifact twice produced different signatures")
	}

	if err := verifyArtifact(artifact, first[0], publicKey); err != nil {
		t.Fatalf("verifyArtifact() error = %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(firstPayload)))
	if err != nil {
		t.Fatalf("signature is not base64: %v", err)
	}
	if len(decoded) != ed25519.SignatureSize {
		t.Fatalf("signature has %d bytes, want %d", len(decoded), ed25519.SignatureSize)
	}
}

func TestVerifyFailsOnModifiedArtifact(t *testing.T) {
	privateKey, publicKey := testKeypair(t)
	artifact := writeArtifact(t, "artifact.bin", []byte("release payload"))
	signature := signOne(t, privateKey, artifact)

	if err := os.WriteFile(artifact, []byte("release payload!"), 0o644); err != nil {
		t.Fatalf("modify artifact: %v", err)
	}

	if err := verifyArtifact(artifact, signature, publicKey); err == nil {
		t.Fatal("verifyArtifact() accepted a modified artifact")
	}
}

func TestVerifyFailsOnModifiedSignature(t *testing.T) {
	privateKey, publicKey := testKeypair(t)
	artifact := writeArtifact(t, "artifact.bin", []byte("release payload"))
	signature := signOne(t, privateKey, artifact)

	payload, err := os.ReadFile(signature)
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(payload)))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	decoded[0] ^= 0xff
	tampered := base64.StdEncoding.EncodeToString(decoded) + "\n"
	if err := os.WriteFile(signature, []byte(tampered), 0o644); err != nil {
		t.Fatalf("write tampered signature: %v", err)
	}

	if err := verifyArtifact(artifact, signature, publicKey); err == nil {
		t.Fatal("verifyArtifact() accepted a modified signature")
	}
}

func TestVerifyFailsWithTheWrongPublicKey(t *testing.T) {
	signingKey, _ := testKeypair(t)
	_, otherPublicKey := testKeypair(t)
	artifact := writeArtifact(t, "artifact.bin", []byte("release payload"))
	signature := signOne(t, signingKey, artifact)

	if err := verifyArtifact(artifact, signature, otherPublicKey); err == nil {
		t.Fatal("verifyArtifact() accepted the wrong public key")
	}
}

func TestVerifyRejectsMalformedBase64(t *testing.T) {
	privateKey, publicKey := testKeypair(t)
	artifact := writeArtifact(t, "artifact.bin", []byte("release payload"))
	signature := signOne(t, privateKey, artifact)

	t.Run("signature", func(t *testing.T) {
		malformed := signature + ".bad"
		if err := os.WriteFile(malformed, []byte("not base64 !!!"), 0o644); err != nil {
			t.Fatalf("write malformed signature: %v", err)
		}
		if err := verifyArtifact(artifact, malformed, publicKey); err == nil {
			t.Fatal("verifyArtifact() accepted a malformed signature")
		}
	})

	t.Run("public key", func(t *testing.T) {
		if err := verifyArtifact(artifact, signature, "not base64 !!!"); err == nil {
			t.Fatal("verifyArtifact() accepted a malformed public key")
		}
	})

	t.Run("wrong length public key", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString([]byte("short"))
		if err := verifyArtifact(artifact, signature, short); err == nil {
			t.Fatal("verifyArtifact() accepted a short public key")
		}
	})
}

func TestMissingFilesAreClearErrors(t *testing.T) {
	privateKey, publicKey := testKeypair(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.bin")
	signature := filepath.Join(dir, "missing.bin.sig")

	if _, err := signArtifacts(privateKey, []string{missing}); err == nil {
		t.Fatal("signArtifacts() accepted a missing artifact")
	} else if !strings.Contains(err.Error(), missing) {
		t.Fatalf("signArtifacts() error %q does not name the missing artifact", err)
	}

	if err := verifyArtifact(missing, signature, publicKey); err == nil {
		t.Fatal("verifyArtifact() accepted a missing artifact")
	} else if !strings.Contains(err.Error(), missing) {
		t.Fatalf("verifyArtifact() error %q does not name the missing artifact", err)
	}

	if err := verifyArtifact(writeArtifact(t, "present.bin", []byte("x")), signature, publicKey); err == nil {
		t.Fatal("verifyArtifact() accepted a missing signature file")
	} else if !strings.Contains(err.Error(), signature) {
		t.Fatalf("verifyArtifact() error %q does not name the missing signature", err)
	}
}

func TestSignRefusesAKeyInsideTheRepository(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Skipf("not inside a Git checkout: %v", err)
	}

	if err := ensureOutsideRepository(filepath.Join(root, "release.key")); err == nil {
		t.Fatal("ensureOutsideRepository() accepted a key inside the repository")
	}

	outside := filepath.Join(t.TempDir(), "release.key")
	if err := ensureOutsideRepository(outside); err != nil {
		t.Fatalf("ensureOutsideRepository(%q) error = %v", outside, err)
	}
}

func TestParseArgsAcceptsTheDocumentedForms(t *testing.T) {
	sign, err := parseArgs([]string{"-sign", "a.bin", "b.bin", "-key", "/offline/release.key"})
	if err != nil {
		t.Fatalf("parseArgs(-sign) error = %v", err)
	}
	if sign.mode != modeSign || sign.key != "/offline/release.key" {
		t.Fatalf("parseArgs(-sign) = %+v", sign)
	}
	if len(sign.artifacts) != 2 || sign.artifacts[0] != "a.bin" || sign.artifacts[1] != "b.bin" {
		t.Fatalf("parseArgs(-sign) artifacts = %v", sign.artifacts)
	}

	verify, err := parseArgs([]string{"-verify", "a.bin", "-sig", "a.bin.sig", "-pubkey", "abc"})
	if err != nil {
		t.Fatalf("parseArgs(-verify) error = %v", err)
	}
	if verify.mode != modeVerify || len(verify.artifacts) != 1 || verify.artifacts[0] != "a.bin" {
		t.Fatalf("parseArgs(-verify) = %+v", verify)
	}
	if verify.sig != "a.bin.sig" || verify.pubkey != "abc" {
		t.Fatalf("parseArgs(-verify) fields = %+v", verify)
	}

	if _, err := parseArgs([]string{"-sign", "-verify", "a"}); err == nil {
		t.Fatal("parseArgs() accepted conflicting modes")
	}
}

func testKeypair(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()

	prefix := filepath.Join(t.TempDir(), "release")
	encodedPublic, err := generateKeyPair(prefix)
	if err != nil {
		t.Fatalf("generateKeyPair() error = %v", err)
	}
	privateKey, err := loadPrivateKey(prefix + privateKeySuffix)
	if err != nil {
		t.Fatalf("loadPrivateKey() error = %v", err)
	}

	return privateKey, encodedPublic
}

func signOne(t *testing.T, key ed25519.PrivateKey, artifact string) string {
	t.Helper()

	paths, err := signArtifacts(key, []string{artifact})
	if err != nil {
		t.Fatalf("signArtifacts() error = %v", err)
	}
	return paths[0]
}

func writeArtifact(t *testing.T, name string, payload []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return path
}
