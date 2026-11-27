package toolchain

import (
	"os/exec"
	"strings"
)

// GhostscriptBinaryNames is the platform-agnostic Ghostscript search order. The
// Unix name comes first; the Windows console builds follow because that is what
// the official installer and the managed conda environment install there. Trying
// every name on every platform is harmless: a name that does not exist is simply
// not found, and keeping one flat list means the toolchain installer, the
// capability panel, the PDF optimize path, the DOCX PDF/A step and the archive
// recipe can never disagree about what "Ghostscript installed" means.
var GhostscriptBinaryNames = []string{"gs", "gswin64c", "gswin32c"}

// GhostscriptBinary is a resolved Ghostscript executable. Name is the candidate
// that matched (for diagnostics) and Path is what the lookup returned (for
// execution).
type GhostscriptBinary struct {
	Name string
	Path string
}

// LookPathFunc mirrors exec.LookPath so callers and tests can inject a PATH.
type LookPathFunc func(name string) (string, error)

// ResolveGhostscript returns the first candidate in GhostscriptBinaryNames that
// lookPath finds. A nil lookPath uses exec.LookPath. The bool is false when no
// supported name exists.
func ResolveGhostscript(lookPath LookPathFunc) (GhostscriptBinary, bool) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	for _, name := range GhostscriptBinaryNames {
		path, err := lookPath(name)
		if err == nil && strings.TrimSpace(path) != "" {
			return GhostscriptBinary{Name: name, Path: path}, true
		}
	}

	return GhostscriptBinary{}, false
}
