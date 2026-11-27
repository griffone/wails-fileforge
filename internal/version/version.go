// Package version holds the build identity of the running binary.
//
// The three variables are filled at link time by the build scripts (Taskfiles
// and scripts/build-windows.sh) with `-X` ldflags. A plain `go build` with no
// injection keeps the development defaults below, so a source build can never
// be mistaken for a release.
package version

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// DevVersion marks a build that was not stamped with a release version.
	DevVersion = "dev"
	// UnknownCommit marks a build whose commit could not be resolved.
	UnknownCommit = "none"
	// UnknownBuiltAt marks a build whose timestamp was not stamped.
	UnknownBuiltAt = "unknown"
)

// Version is the release identity. Release builds inject a git tag or a
// semver value; local builds keep DevVersion. It may carry a leading "v".
var Version = DevVersion

// Commit is the short git commit the binary was built from.
var Commit = UnknownCommit

// BuiltAt is the UTC build timestamp in RFC 3339 format.
var BuiltAt = UnknownBuiltAt

// semverPattern matches a SemVer 2.0.0 version with an optional leading "v".
var semverPattern = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// gitDescribePattern matches the commit-distance form git describe emits when
// HEAD is not exactly on a tag, for example v1.1.0-41-g35d1007. It is valid
// semver on paper but it is not a release, so IsRelease must reject it.
var gitDescribePattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+-\d+-g[0-9a-fA-F]+$`)

// Info is a normalized snapshot of the build identity.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
	Release bool   `json:"release"`
}

// Current returns the normalized build identity. Empty values fall back to the
// development defaults and Release reports whether the version can drive an
// update check.
func Current() Info {
	return Info{
		Version: Number(),
		Commit:  commitValue(),
		BuiltAt: builtAtValue(),
		Release: IsRelease(),
	}
}

// Number returns the version without a leading "v" and with the development
// default applied. It is the value suitable for MCP serverInfo and release
// comparisons.
func Number() string {
	value := strings.TrimSpace(Version)
	if value == "" {
		return DevVersion
	}
	if len(value) > 1 && value[0] == 'v' && value[1] >= '0' && value[1] <= '9' {
		return value[1:]
	}
	return value
}

// String renders the full identity, for example
// "1.2.0 (a1b2c3d, 2026-11-26T12:00:00Z)".
func String() string {
	info := Current()
	return fmt.Sprintf("%s (%s, %s)", info.Version, info.Commit, info.BuiltAt)
}

// IsRelease reports whether Version is a real release version that an updater
// may compare. Development builds, dirty trees, git-describe commit distances
// and anything that is not SemVer return false.
func IsRelease() bool {
	value := strings.TrimSpace(Version)
	if value == "" || strings.EqualFold(value, DevVersion) {
		return false
	}
	if strings.Contains(strings.ToLower(value), "dirty") {
		return false
	}
	if gitDescribePattern.MatchString(value) {
		return false
	}
	return semverPattern.MatchString(value)
}

func commitValue() string {
	value := strings.TrimSpace(Commit)
	if value == "" {
		return UnknownCommit
	}
	return value
}

func builtAtValue() string {
	value := strings.TrimSpace(BuiltAt)
	if value == "" {
		return UnknownBuiltAt
	}
	return value
}
