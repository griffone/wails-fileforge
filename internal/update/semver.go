package update

import (
	"strconv"
	"strings"
)

// semverValue is the subset of SemVer the updater needs: a numeric triple and
// an optional prerelease. Build metadata is ignored, as SemVer requires.
type semverValue struct {
	major, minor, patch int
	prerelease          []string
}

// parseSemver accepts an optional leading "v", three numeric components and an
// optional prerelease. It is deliberately strict: a version the updater cannot
// order is not a version it will install.
func parseSemver(value string) (semverValue, bool) {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "v")
	if trimmed == "" {
		return semverValue{}, false
	}
	if index := strings.IndexByte(trimmed, '+'); index >= 0 {
		trimmed = trimmed[:index]
	}

	var prerelease []string
	if index := strings.IndexByte(trimmed, '-'); index >= 0 {
		prerelease = strings.Split(trimmed[index+1:], ".")
		for _, part := range prerelease {
			if part == "" {
				return semverValue{}, false
			}
		}
		trimmed = trimmed[:index]
	}

	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return semverValue{}, false
	}
	numbers := make([]int, 3)
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semverValue{}, false
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semverValue{}, false
		}
		numbers[index] = number
	}

	return semverValue{major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: prerelease}, true
}

// IsNewer reports whether candidate is a strict upgrade over current. An
// unparsable current version (a dev build or an unknown stamp) treats any
// parsable candidate as newer; an unparsable candidate is never newer. Equal
// versions are not newer, so a stable release is never re-offered.
func IsNewer(candidate, current string) bool {
	next, ok := parseSemver(candidate)
	if !ok {
		return false
	}
	installed, ok := parseSemver(current)
	if !ok {
		return true
	}
	return compareSemver(next, installed) > 0
}

// CompareVersions compares two versions and reports -1, 0 or 1 plus whether
// both could be parsed.
func CompareVersions(left, right string) (int, bool) {
	a, ok := parseSemver(left)
	if !ok {
		return 0, false
	}
	b, ok := parseSemver(right)
	if !ok {
		return 0, false
	}
	return compareSemver(a, b), true
}

func compareSemver(a, b semverValue) int {
	if result := compareInt(a.major, b.major); result != 0 {
		return result
	}
	if result := compareInt(a.minor, b.minor); result != 0 {
		return result
	}
	if result := compareInt(a.patch, b.patch); result != 0 {
		return result
	}
	return comparePrerelease(a.prerelease, b.prerelease)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// comparePrerelease follows SemVer 2.0.0 rule 11: a version without a
// prerelease outranks one with it; numeric identifiers compare numerically and
// rank below alphanumeric ones.
func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}

	for index := 0; index < len(a) && index < len(b); index++ {
		if result := compareIdentifier(a[index], b[index]); result != 0 {
			return result
		}
	}
	return compareInt(len(a), len(b))
}

func compareIdentifier(a, b string) int {
	aNumber, aErr := strconv.Atoi(a)
	bNumber, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return compareInt(aNumber, bNumber)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}
