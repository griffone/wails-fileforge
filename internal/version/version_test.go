package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestString(t *testing.T) {
	tests := []struct {
		name    string
		version string
		commit  string
		builtAt string
		want    string
	}{
		{
			name:    "release build",
			version: "1.2.0",
			commit:  "a1b2c3d",
			builtAt: "2026-11-26T12:00:00Z",
			want:    "1.2.0 (a1b2c3d, 2026-11-26T12:00:00Z)",
		},
		{
			name:    "leading v is dropped from the display value",
			version: "v1.2.0",
			commit:  "a1b2c3d",
			builtAt: "2026-11-26T12:00:00Z",
			want:    "1.2.0 (a1b2c3d, 2026-11-26T12:00:00Z)",
		},
		{
			name:    "empty values use the development defaults",
			version: "",
			commit:  "",
			builtAt: "",
			want:    "dev (none, unknown)",
		},
		{
			name:    "whitespace is trimmed",
			version: "  1.2.0  ",
			commit:  " a1b2c3d ",
			builtAt: " 2026-11-26T12:00:00Z ",
			want:    "1.2.0 (a1b2c3d, 2026-11-26T12:00:00Z)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restore := setIdentity(t, test.version, test.commit, test.builtAt)
			t.Cleanup(restore)

			if got := String(); got != test.want {
				t.Fatalf("String() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIsRelease(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "release", version: "1.2.0", want: true},
		{name: "release with v prefix", version: "v1.2.0", want: true},
		{name: "release candidate", version: "1.2.0-rc.1", want: true},
		{name: "build metadata", version: "1.2.0+build.5", want: true},
		{name: "dev", version: "dev", want: false},
		{name: "dev uppercase", version: "DEV", want: false},
		{name: "empty", version: "", want: false},
		{name: "dirty release", version: "1.2.0-dirty", want: false},
		{name: "dirty git describe", version: "v1.1.0-41-g35d1007-dirty", want: false},
		{name: "git describe commit distance", version: "v1.1.0-41-g35d1007", want: false},
		{name: "short commit", version: "35d1007", want: false},
		{name: "incomplete semver", version: "1.2", want: false},
		{name: "four components", version: "1.2.0.0", want: false},
		{name: "leading zero", version: "1.02.0", want: false},
		{name: "word", version: "latest", want: false},
		{name: "whitespace around release", version: " 1.2.0 ", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			restore := setIdentity(t, test.version, "a1b2c3d", "2026-11-26T12:00:00Z")
			t.Cleanup(restore)

			if got := IsRelease(); got != test.want {
				t.Fatalf("IsRelease() for %q = %v, want %v", test.version, got, test.want)
			}
		})
	}
}

func TestCurrentNormalizesTheIdentity(t *testing.T) {
	restore := setIdentity(t, "v1.2.0", "a1b2c3d", "2026-11-26T12:00:00Z")
	t.Cleanup(restore)

	info := Current()
	if info.Version != "1.2.0" || info.Commit != "a1b2c3d" || info.BuiltAt != "2026-11-26T12:00:00Z" || !info.Release {
		t.Fatalf("Current() = %+v", info)
	}
}

// TestReleaseMetadataStaysInSync keeps the release-facing copies of the
// version aligned. wails.json is the source the installers and the source
// tarball fallback read; build/config.yml and build/windows/info.json feed the
// Wails and Windows metadata. When a release version is injected into the test
// binary, it must match wails.json too, so a mistagged release fails here.
func TestReleaseMetadataStaysInSync(t *testing.T) {
	root := filepath.Join("..", "..")

	var manifest struct {
		Version string `json:"version"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	readJSON(t, filepath.Join(root, "wails.json"), &manifest)

	if manifest.Version == "" {
		t.Fatal("wails.json has no version")
	}
	if manifest.Info.Version != manifest.Version {
		t.Fatalf("wails.json info.version = %q, want %q", manifest.Info.Version, manifest.Version)
	}

	configVersion := configInfoVersion(t, filepath.Join(root, "build", "config.yml"))
	if configVersion != manifest.Version {
		t.Fatalf("build/config.yml info version = %q, want %q", configVersion, manifest.Version)
	}

	var windowsInfo struct {
		Fixed struct {
			FileVersion string `json:"file_version"`
		} `json:"fixed"`
		Info map[string]struct {
			ProductVersion string `json:"ProductVersion"`
		} `json:"info"`
	}
	readJSON(t, filepath.Join(root, "build", "windows", "info.json"), &windowsInfo)
	if windowsInfo.Fixed.FileVersion != manifest.Version {
		t.Fatalf("build/windows/info.json file_version = %q, want %q", windowsInfo.Fixed.FileVersion, manifest.Version)
	}
	for key, block := range windowsInfo.Info {
		if block.ProductVersion != "" && block.ProductVersion != manifest.Version {
			t.Fatalf("build/windows/info.json info[%s].ProductVersion = %q, want %q", key, block.ProductVersion, manifest.Version)
		}
	}

	if IsRelease() && Number() != manifest.Version {
		t.Fatalf("injected release version %q does not match wails.json %q; bump the release metadata", Number(), manifest.Version)
	}
}

func setIdentity(t *testing.T, value, commit, builtAt string) func() {
	t.Helper()

	previousVersion, previousCommit, previousBuiltAt := Version, Commit, BuiltAt
	Version = value
	Commit = commit
	BuiltAt = builtAt

	return func() {
		Version = previousVersion
		Commit = previousCommit
		BuiltAt = previousBuiltAt
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

var configInfoVersionPattern = regexp.MustCompile(`(?m)^[ \t]{2}version:\s*"([^"]+)"`)

func configInfoVersion(t *testing.T, path string) string {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	match := configInfoVersionPattern.FindSubmatch(payload)
	if len(match) != 2 {
		t.Fatalf("no info.version found in %s", path)
	}
	return string(match[1])
}
