package automation

import (
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
)

// ruleFilter implements the watch-folder path filters in one place. The
// watcher and the organize preview both use it, so a file that the preview
// lists is exactly a file the watcher would accept.
type ruleFilter struct {
	watchDir  string
	recursive bool

	extensions map[string]struct{}
	// anyExtension accepts every file when extensions is empty. Only organize
	// rules can reach it: a recipe rule derives its extensions from the recipe,
	// and every built-in recipe declares at least one.
	anyExtension bool

	// patterns are base-name globs; an empty list means no name filter.
	patterns []string
	// excludeDir is a folder subtree that is never accepted, used by recipe
	// rules that write into a subfolder of the watched folder.
	excludeDir string
}

// newRuleFilter builds the filter for a rule.
func newRuleFilter(rule models.AutomationRule) ruleFilter {
	extensions := make(map[string]struct{}, len(rule.Extensions))
	for _, extension := range rule.Extensions {
		if normalized := normalizeExtension(extension); normalized != "" {
			extensions[normalized] = struct{}{}
		}
	}

	patterns := make([]string, 0, len(rule.Patterns))
	for _, pattern := range rule.Patterns {
		if trimmed := strings.TrimSpace(pattern); trimmed != "" {
			patterns = append(patterns, trimmed)
		}
	}

	excludeDir := ""
	if rule.Action != models.AutomationActionOrganize &&
		strings.TrimSpace(rule.OutputDir) != "" &&
		rule.OutputDir != rule.WatchDir &&
		pathWithin(rule.WatchDir, rule.OutputDir) {
		excludeDir = rule.OutputDir
	}

	return ruleFilter{
		watchDir:     rule.WatchDir,
		recursive:    rule.Recursive,
		extensions:   extensions,
		anyExtension: rule.Action == models.AutomationActionOrganize && len(extensions) == 0,
		patterns:     patterns,
		excludeDir:   excludeDir,
	}
}

// acceptsPath applies the path, hidden/temp, extension, pattern and excluded
// folder filters. Recursive filters accept any depth; other filters only accept
// direct children. It returns the stat info so callers never repeat the
// filesystem call.
func (f ruleFilter) acceptsPath(path string) (os.FileInfo, bool) {
	relative, err := filepath.Rel(f.watchDir, path)
	if err != nil {
		return nil, false
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, false
	}
	// Non-recursive: only direct children of the watched folder are accepted.
	if !f.recursive && strings.ContainsRune(relative, filepath.Separator) {
		return nil, false
	}
	// Hidden and in-progress names are ignored at any depth.
	if hasIgnoredSegment(relative) {
		return nil, false
	}

	if f.excludeDir != "" && pathWithin(f.excludeDir, path) {
		return nil, false
	}

	base := filepath.Base(relative)
	if len(f.extensions) > 0 {
		extension := normalizeExtension(filepath.Ext(base))
		if extension == "" {
			return nil, false
		}
		if _, accepted := f.extensions[extension]; !accepted {
			return nil, false
		}
	} else if !f.anyExtension {
		return nil, false
	}

	// Patterns and extensions are independent filters: when both are set, a
	// file must satisfy both.
	if !f.matchesPatterns(base) {
		return nil, false
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, false
	}

	return info, true
}

// skipsDirectory rejects hidden/temp folders, paths outside the watched tree
// and the excluded subtree in recursive mode. Symlinked directories are never
// walked because os.Stat follows links while filepath.WalkDir does not, which
// keeps cycles out.
func (f ruleFilter) skipsDirectory(path string) bool {
	if isIgnoredName(filepath.Base(path)) {
		return true
	}
	if !pathWithin(f.watchDir, path) {
		return true
	}
	if f.excludeDir != "" && pathWithin(f.excludeDir, path) {
		return true
	}

	return false
}

// matchesPatterns reports whether base matches at least one configured glob.
// An empty pattern list means no name filter. Invalid patterns never match;
// rule save rejects them, so a hand-edited store cannot crash the watcher.
func (f ruleFilter) matchesPatterns(base string) bool {
	if len(f.patterns) == 0 {
		return true
	}
	for _, pattern := range f.patterns {
		if matched, err := filepath.Match(pattern, base); err == nil && matched {
			return true
		}
	}

	return false
}
