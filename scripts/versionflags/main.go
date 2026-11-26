// Command versionflags resolves the build identity and prints the ldflags that
// stamp it into a binary.
//
// Every build path calls this one program: Taskfile.yml (build:cli), the Linux,
// Windows and macOS Taskfiles, and scripts/build-windows.sh. That keeps the
// version and commit from one source and the build timestamp from one clock.
//
// Usage:
//
//	go run ./scripts/versionflags            # prints the -X ldflags
//	go run ./scripts/versionflags -version   # prints only the version
//	go run ./scripts/versionflags -commit    # prints only the commit
//	go run ./scripts/versionflags -built-at  # prints only the build time
//
// Set FILEFORGE_UPDATE_PUBKEY to the base64 ed25519 release key to add
// `-X fileforge-desktop/internal/update.PublicKey=...` to the flags. Without
// it the binary has no update trust anchor and update checking stays disabled.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	updatepkg "fileforge-desktop/internal/update"
	appversion "fileforge-desktop/internal/version"
)

const (
	versionPackage = "fileforge-desktop/internal/version"
	updatePackage  = "fileforge-desktop/internal/update"

	// updatePublicKeyEnv carries the base64 ed25519 public key that release
	// signatures are verified against. It is optional: an empty value leaves
	// update.PublicKey empty, which disables update checking in the build.
	updatePublicKeyEnv = "FILEFORGE_UPDATE_PUBKEY"
)

type buildInfo struct {
	version string
	commit  string
	builtAt string
}

func main() {
	mode := "ldflags"
	if len(os.Args) > 1 {
		mode = strings.TrimLeft(os.Args[1], "-")
	}

	info := resolve()

	switch mode {
	case "ldflags":
		flags, err := buildLDFlags(info, os.Getenv(updatePublicKeyEnv))
		if err != nil {
			fmt.Fprintf(os.Stderr, "versionflags: %v\n", err)
			os.Exit(2)
		}
		fmt.Print(flags)
	case "version":
		fmt.Println(info.version)
	case "commit":
		fmt.Println(info.commit)
	case "built-at":
		fmt.Println(info.builtAt)
	default:
		fmt.Fprintf(os.Stderr, "versionflags: unknown field %q (want ldflags, version, commit or built-at)\n", mode)
		os.Exit(2)
	}
}

// buildLDFlags renders the -X flags for the build identity and, when a public
// key is supplied, the update trust anchor. A malformed key fails the build
// instead of producing a binary that trusts an undecodable value.
func buildLDFlags(info buildInfo, publicKey string) (string, error) {
	flags := fmt.Sprintf(
		"-X %s.Version=%s -X %s.Commit=%s -X %s.BuiltAt=%s",
		versionPackage, info.version,
		versionPackage, info.commit,
		versionPackage, info.builtAt,
	)

	trimmedKey := strings.TrimSpace(publicKey)
	if trimmedKey == "" {
		return flags, nil
	}
	if _, err := updatepkg.ParsePublicKey(trimmedKey); err != nil {
		return "", fmt.Errorf("%s: %w", updatePublicKeyEnv, err)
	}

	return fmt.Sprintf("%s -X %s.PublicKey=%s", flags, updatePackage, trimmedKey), nil
}

// resolve prefers git, then falls back to the release-facing wails.json for a
// source tarball that has no .git directory. The build time always comes from
// the current clock so even a tarball build is stamped.
func resolve() buildInfo {
	info := buildInfo{
		version: appversion.DevVersion,
		commit:  appversion.UnknownCommit,
		builtAt: time.Now().UTC().Format(time.RFC3339),
	}

	if value, err := gitOutput("describe", "--tags", "--always", "--dirty"); err == nil && value != "" {
		info.version = value
	} else if value := wailsVersion(); value != "" {
		info.version = value
	}

	if value, err := gitOutput("rev-parse", "--short", "HEAD"); err == nil && value != "" {
		info.commit = value
	}

	return info
}

func gitOutput(args ...string) (string, error) {
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// wailsVersion reads the release-facing version from wails.json. It searches
// upward so the helper works from any Taskfile directory.
func wailsVersion() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		path := filepath.Join(dir, "wails.json")
		if payload, readErr := os.ReadFile(path); readErr == nil {
			var manifest struct {
				Version string `json:"version"`
			}
			if jsonErr := json.Unmarshal(payload, &manifest); jsonErr == nil {
				return strings.TrimSpace(manifest.Version)
			}
			return ""
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
