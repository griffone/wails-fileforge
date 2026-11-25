package fsx

import (
	"strings"
	"unicode"
)

const (
	// MaxBaseNameLength bounds a suggested base name so a long document title
	// never produces a path the host filesystem rejects or the UI cannot show.
	MaxBaseNameLength = 120

	// reservedNameSuffix keeps a Windows device name usable as a file name.
	reservedNameSuffix = "_"
)

// forbiddenBaseNameRunes are rejected by Windows on every filesystem and can
// break path handling on Unix too, so they are replaced with a space.
const forbiddenBaseNameRunes = `<>:"/\|?*`

// windowsReservedBaseNames are device names Windows refuses as file names,
// with or without an extension.
var windowsReservedBaseNames = map[string]struct{}{
	"con":  {},
	"prn":  {},
	"aux":  {},
	"nul":  {},
	"com1": {},
	"com2": {},
	"com3": {},
	"com4": {},
	"com5": {},
	"com6": {},
	"com7": {},
	"com8": {},
	"com9": {},
	"lpt1": {},
	"lpt2": {},
	"lpt3": {},
	"lpt4": {},
	"lpt5": {},
	"lpt6": {},
	"lpt7": {},
	"lpt8": {},
	"lpt9": {},
}

// SanitizeBaseName turns arbitrary text into a single path segment that is
// valid on Windows and Unix. Forbidden characters and control runes become a
// space, whitespace runs collapse to one space, leading and trailing spaces or
// dots are removed, and the result is capped at MaxBaseNameLength runes.
// Windows device names get a trailing underscore. An input without any usable
// character returns an empty string; callers decide whether to fall back to
// another candidate.
func SanitizeBaseName(value string) string {
	var builder strings.Builder
	lastWasSpace := false

	writeSpace := func() {
		if builder.Len() == 0 || lastWasSpace {
			return
		}
		builder.WriteByte(' ')
		lastWasSpace = true
	}

	for _, char := range strings.TrimSpace(value) {
		switch {
		case unicode.IsSpace(char), unicode.IsControl(char):
			writeSpace()
		case strings.ContainsRune(forbiddenBaseNameRunes, char):
			writeSpace()
		default:
			builder.WriteRune(char)
			lastWasSpace = false
		}
	}

	name := strings.Trim(builder.String(), " .")
	name = truncateRunes(name, MaxBaseNameLength)
	name = strings.TrimRight(name, " .")
	if name == "" {
		return ""
	}

	if isWindowsReservedBaseName(name) {
		return name + reservedNameSuffix
	}

	return name
}

// truncateRunes cuts value to at most limit runes without splitting one.
func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}

	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}

	return string(runes[:limit])
}

// isWindowsReservedBaseName reports whether the stem (before the first dot)
// names a Windows device.
func isWindowsReservedBaseName(name string) bool {
	stem := name
	if index := strings.IndexByte(stem, '.'); index >= 0 {
		stem = stem[:index]
	}

	_, reserved := windowsReservedBaseNames[strings.ToLower(stem)]

	return reserved
}
