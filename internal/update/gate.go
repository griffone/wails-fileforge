package update

import (
	"os"
	"runtime"
	"strings"

	"fileforge-desktop/internal/version"
)

// Install methods the service can perform. An empty method means the build is
// not installable; Method is only populated when Gate.Available is true.
const (
	MethodNone             = ""
	MethodAppImage         = "appimage"
	MethodWindowsInstaller = "windows-installer"
)

// Gating reasons. They are user-facing copy: the Settings panel shows them
// verbatim, and tests pin them so the honest reason cannot silently drift.
const (
	ReasonDevBuild            = "This is a development build. Updates are only available in release builds."
	ReasonNoPublicKey         = "This build has no update signing key pinned, so updates are disabled."
	ReasonUnsupportedPlatform = "Updates are not supported on this platform."
	ReasonPackageManager      = "This installation is managed by your package manager. Update FileForge with it."
)

// Environment is every input the installability decision reads. It is a value
// type so tests can evaluate the full table without touching global build
// identity or the real environment.
type Environment struct {
	GOOS          string
	GOARCH        string
	Version       string
	Release       bool
	KeyConfigured bool
	// AppImage is the path from $APPIMAGE. It is set only when the running
	// app is an AppImage; deb, rpm and Arch installs leave it empty.
	AppImage string
}

// Gate is the server-side installability decision. Available is the single
// source of truth: when it is false the service never offers or starts a
// download, regardless of what a release feed claims.
type Gate struct {
	Available bool   `json:"available"`
	Method    string `json:"method,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// CurrentEnvironment reads the running build identity and install type.
func CurrentEnvironment() Environment {
	_, keyErr := Key()
	return Environment{
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		Version:       version.Number(),
		Release:       version.IsRelease(),
		KeyConfigured: keyErr == nil,
		AppImage:      strings.TrimSpace(os.Getenv("APPIMAGE")),
	}
}

// EvaluateGate decides whether this build and install type can update itself.
//
// The order is deliberate: a development build has no trusted identity, a
// build without a pinned key has no trust anchor, and only then does the
// platform matter. Every unavailable answer carries a reason; there is no
// "available but unverified" state.
func EvaluateGate(env Environment) Gate {
	if !env.Release {
		return Gate{Reason: ReasonDevBuild}
	}
	if !env.KeyConfigured {
		return Gate{Reason: ReasonNoPublicKey}
	}

	switch {
	case env.GOOS == "linux" && env.GOARCH == "amd64":
		if strings.TrimSpace(env.AppImage) == "" {
			return Gate{Reason: ReasonPackageManager}
		}
		return Gate{Available: true, Method: MethodAppImage}
	case env.GOOS == "windows" && env.GOARCH == "amd64":
		return Gate{Available: true, Method: MethodWindowsInstaller}
	default:
		return Gate{Reason: ReasonUnsupportedPlatform}
	}
}
