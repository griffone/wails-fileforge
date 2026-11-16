package recipes

import (
	"path/filepath"
	"strings"
)

// recipeSourceStemFallback is used when the source name has no safe
// characters left after sanitizing.
const recipeSourceStemFallback = "image"

// recipeSourceStem turns the source file name into a web-friendly base name
// shared by the fan-out recipes: lowercase ASCII letters, digits, dots,
// underscores and dashes survive; every other run is collapsed into a single
// dash. This keeps spaces and ampersands out of folder names and URLs.
func recipeSourceStem(inputPath string) string {
	raw := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	var builder strings.Builder
	lastDash := false
	for _, char := range strings.ToLower(raw) {
		safe := (char >= 'a' && char <= 'z') ||
			(char >= '0' && char <= '9') ||
			char == '.' || char == '_' || char == '-'
		if safe {
			builder.WriteRune(char)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}

	stem := strings.Trim(builder.String(), "-._")
	if stem == "" {
		return recipeSourceStemFallback
	}

	return stem
}
