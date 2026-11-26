package update

import (
	"testing"

	"fileforge-desktop/internal/version"
)

func TestCurrentEnvironmentRequiresAUsableKey(t *testing.T) {
	previousVersion := version.Version
	version.Version = "1.2.0"
	t.Cleanup(func() { version.Version = previousVersion })

	restore := setPublicKey(t, "not base64")
	t.Cleanup(restore)

	environment := CurrentEnvironment()
	if environment.KeyConfigured {
		t.Fatal("CurrentEnvironment accepted a malformed pinned key")
	}
	if gate := EvaluateGate(environment); gate.Available || gate.Reason != ReasonNoPublicKey {
		t.Fatalf("gate = %+v, want the no-key reason", gate)
	}
}

func TestEvaluateGate(t *testing.T) {
	base := Environment{
		GOOS:          "linux",
		GOARCH:        "amd64",
		Version:       "1.2.0",
		Release:       true,
		KeyConfigured: true,
		AppImage:      "/opt/FileForge.AppImage",
	}

	tests := []struct {
		name        string
		mutate      func(*Environment)
		wantEnabled bool
		wantMethod  string
		wantReason  string
	}{
		{
			name:       "development build",
			mutate:     func(env *Environment) { env.Version = "dev"; env.Release = false },
			wantReason: ReasonDevBuild,
		},
		{
			name:       "release build without a pinned key",
			mutate:     func(env *Environment) { env.KeyConfigured = false },
			wantReason: ReasonNoPublicKey,
		},
		{
			name:       "unsupported operating system",
			mutate:     func(env *Environment) { env.GOOS = "darwin" },
			wantReason: ReasonUnsupportedPlatform,
		},
		{
			name:       "unsupported architecture",
			mutate:     func(env *Environment) { env.GOARCH = "arm64" },
			wantReason: ReasonUnsupportedPlatform,
		},
		{
			name:       "linux package install without APPIMAGE",
			mutate:     func(env *Environment) { env.AppImage = "" },
			wantReason: ReasonPackageManager,
		},
		{
			name:        "linux AppImage",
			mutate:      func(*Environment) {},
			wantEnabled: true,
			wantMethod:  MethodAppImage,
		},
		{
			name: "windows amd64",
			mutate: func(env *Environment) {
				env.GOOS = "windows"
				env.AppImage = ""
			},
			wantEnabled: true,
			wantMethod:  MethodWindowsInstaller,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			environment := base
			test.mutate(&environment)

			gate := EvaluateGate(environment)
			if gate.Available != test.wantEnabled {
				t.Fatalf("Available = %v, want %v (reason %q)", gate.Available, test.wantEnabled, gate.Reason)
			}
			if gate.Method != test.wantMethod {
				t.Fatalf("Method = %q, want %q", gate.Method, test.wantMethod)
			}
			if gate.Reason != test.wantReason {
				t.Fatalf("Reason = %q, want %q", gate.Reason, test.wantReason)
			}
			if !gate.Available && gate.Reason == "" {
				t.Fatal("an unavailable gate must always carry a reason")
			}
		})
	}
}
