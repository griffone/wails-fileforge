package fsx

import (
	"strings"
	"testing"
)

func TestSanitizeBaseNameReplacesForbiddenCharacters(t *testing.T) {
	cases := map[string]string{
		`Report: Q1/Q2 "final" <draft>|v1?`: "Report Q1 Q2 final draft v1",
		`back\slash*and?more`:               "back slash and more",
		"tab\tand\nnewline":                 "tab and newline",
		"  padded  name  ":                  "padded name",
		"trailing dots...":                  "trailing dots",
	}

	for input, want := range cases {
		if got := SanitizeBaseName(input); got != want {
			t.Errorf("SanitizeBaseName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSanitizeBaseNameCapsLength(t *testing.T) {
	got := SanitizeBaseName(strings.Repeat("a", MaxBaseNameLength+40))
	if runeCount := len([]rune(got)); runeCount != MaxBaseNameLength {
		t.Fatalf("length = %d, want %d", runeCount, MaxBaseNameLength)
	}
}

func TestSanitizeBaseNameHandlesWindowsDeviceNames(t *testing.T) {
	cases := map[string]string{
		"CON":      "CON_",
		"nul":      "nul_",
		"NUL.txt":  "NUL.txt_",
		"console":  "console",
		"CONTRACT": "CONTRACT",
	}

	for input, want := range cases {
		if got := SanitizeBaseName(input); got != want {
			t.Errorf("SanitizeBaseName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSanitizeBaseNameNeverKeepsOnlyForbiddenText(t *testing.T) {
	for _, input := range []string{"", "   ", `<>:"/\|?*`, "...", " . "} {
		if got := SanitizeBaseName(input); got != "" {
			t.Errorf("SanitizeBaseName(%q) = %q, want empty", input, got)
		}
	}
}

func TestSanitizeBaseNameKeepsUnicodeLetters(t *testing.T) {
	if got := SanitizeBaseName("Informe anual — año 2026"); got != "Informe anual — año 2026" {
		t.Fatalf("SanitizeBaseName kept %q", got)
	}
}
