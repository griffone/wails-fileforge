// Package mcp implements FileForge's local Model Context Protocol server.
//
// The server speaks newline-delimited JSON-RPC 2.0 over stdio. It exposes six
// tools that can inspect the catalog, plan a job or recipe, and run it. It never
// exposes automation, organize, duplicate scans, or any permission change.
//
// Every path in a call must resolve, after following symbolic links, inside one
// of the roots the user granted from the terminal. An empty allowlist denies
// everything.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fileforge-desktop/internal/platform/fsx"
)

const (
	// ConfigVersion is the on-disk shape of mcp.json.
	ConfigVersion = 1

	configFileName = "mcp.json"
	configFileMode = 0o644

	// grantCommand is the only way to add a root. Keeping it in one constant
	// makes every denial message actionable and consistent.
	grantCommand = "fileforge-cli mcp allow <dir>"
)

// Config is the versioned mcp.json payload. AllowedRoots are absolute,
// symlink-resolved directories written by `fileforge-cli mcp allow`.
type Config struct {
	Version      int      `json:"version"`
	AllowedRoots []string `json:"allowedRoots"`
}

// DefaultDir returns the FileForge config directory that holds mcp.json and
// mcp_audit.log. It refuses to guess: a missing user config directory is an
// error instead of silently falling back to the current directory, because a
// config planted in the working tree must never grant access.
func DefaultDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve the user config directory: %w", err)
	}
	if strings.TrimSpace(configDir) == "" {
		return "", errors.New("the user config directory is empty")
	}

	return filepath.Join(configDir, "fileforge"), nil
}

// Store owns mcp.json. Reads and writes are serialized so two terminal
// invocations cannot lose an update.
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore creates a store rooted at dir. The directory is created on the first
// write.
func NewStore(dir string) *Store {
	return &Store{path: filepath.Join(dir, configFileName)}
}

// ConfigPath returns the mcp.json location for a config directory.
func ConfigPath(dir string) string {
	return filepath.Join(dir, configFileName)
}

// AuditPath returns the mcp_audit.log location for a config directory.
func AuditPath(dir string) string {
	return filepath.Join(dir, auditFileName)
}

// Load reads the config. A missing file is an empty, valid allowlist.
func (s *Store) Load() (Config, error) {
	if s == nil {
		return Config{}, errors.New("mcp: store is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

// Roots returns the stored allowed roots.
func (s *Store) Roots() ([]string, error) {
	config, err := s.Load()
	if err != nil {
		return nil, err
	}

	return config.AllowedRoots, nil
}

// Allow resolves folder, stores it and returns the normalized absolute path.
// Granting the same folder twice is a no-op. The folder must exist and be a
// directory so a typo cannot create a grant for a path that does not exist yet.
func (s *Store) Allow(folder string) (string, error) {
	normalized, err := NormalizeRoot(folder)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := s.loadLocked()
	if err != nil {
		return "", err
	}

	key := fsx.HostPathKey(normalized)
	for _, root := range config.AllowedRoots {
		if fsx.HostPathKey(root) == key {
			return normalized, nil
		}
	}

	config.AllowedRoots = append(config.AllowedRoots, normalized)
	if err := s.saveLocked(config); err != nil {
		return "", err
	}

	return normalized, nil
}

// Deny removes folder from the allowlist and returns the stored roots that
// matched. The input does not have to exist any more: the lexical absolute path
// and its symlink target (when resolvable) are both matched.
func (s *Store) Deny(folder string) ([]string, error) {
	key, err := loosePathKey(folder)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	config, err := s.loadLocked()
	if err != nil {
		return nil, err
	}

	kept := make([]string, 0, len(config.AllowedRoots))
	removed := make([]string, 0, 1)
	for _, root := range config.AllowedRoots {
		if storedRootKey(root) == key {
			removed = append(removed, root)
			continue
		}
		kept = append(kept, root)
	}
	if len(removed) == 0 {
		return nil, nil
	}

	config.AllowedRoots = kept
	if err := s.saveLocked(config); err != nil {
		return nil, err
	}

	return removed, nil
}

func (s *Store) loadLocked() (Config, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{Version: ConfigVersion, AllowedRoots: []string{}}, nil
		}
		return Config{}, fmt.Errorf("read %s: %w", s.path, err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode %s: %w", s.path, err)
	}
	if config.Version == 0 {
		config.Version = ConfigVersion
	}
	if config.Version != ConfigVersion {
		return Config{}, fmt.Errorf("%s has unsupported version %d (supported: %d)", s.path, config.Version, ConfigVersion)
	}
	if config.AllowedRoots == nil {
		config.AllowedRoots = []string{}
	}

	return config, nil
}

func (s *Store) saveLocked(config Config) error {
	config.Version = ConfigVersion
	if config.AllowedRoots == nil {
		config.AllowedRoots = []string{}
	}

	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", s.path, err)
	}
	payload = append(payload, '\n')

	if err := fsx.WriteAtomic(s.path, payload, configFileMode); err != nil {
		return fmt.Errorf("write %s: %w", s.path, err)
	}

	return nil
}

// NormalizeRoot resolves folder to an absolute, symlink-free directory. It is
// the only accepted form for a stored grant.
func NormalizeRoot(folder string) (string, error) {
	trimmed := strings.TrimSpace(folder)
	if trimmed == "" {
		return "", errors.New("the folder is required")
	}

	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve folder %q: %w", trimmed, err)
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve folder %q: %w", absolute, err)
	}
	resolved = filepath.Clean(resolved)

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect folder %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", resolved)
	}

	return resolved, nil
}

// loosePathKey normalizes a path for matching a stored grant without requiring
// it to exist. When the path or one of its parents resolves, the resolved form
// is used so denying through a symlink also removes the stored target.
func loosePathKey(folder string) (string, error) {
	trimmed := strings.TrimSpace(folder)
	if trimmed == "" {
		return "", errors.New("the folder is required")
	}

	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("resolve folder %q: %w", trimmed, err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}

	return fsx.HostPathKey(absolute), nil
}

// storedRootKey returns the comparison key of a stored root, resolving it again
// so a grant written before a directory was moved is still matched by its
// current target.
func storedRootKey(root string) string {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(trimmed); err == nil {
		trimmed = resolved
	}

	return fsx.HostPathKey(trimmed)
}

// Policy is the effective allowlist used to check request paths. Its roots are
// normalized once at construction.
type Policy struct {
	roots []string
}

// NewPolicy builds a policy from stored roots. Empty and duplicate entries are
// dropped. An empty policy denies every path.
func NewPolicy(roots []string) *Policy {
	normalized := make([]string, 0, len(roots))
	seen := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		candidate := canonicalRoot(root)
		if candidate == "" {
			continue
		}
		key := fsx.HostPathKey(candidate)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, candidate)
	}

	return &Policy{roots: normalized}
}

// Roots returns a copy of the effective roots.
func (p *Policy) Roots() []string {
	if p == nil {
		return nil
	}

	return append([]string{}, p.roots...)
}

// Check reports whether path resolves inside one allowed root. A relative path
// is resolved against the process working directory first, so it must still
// land inside a grant. Paths that use ".." to escape or a symlink whose target
// is outside are rejected.
func (p *Policy) Check(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("path is empty")
	}
	if p == nil || len(p.roots) == 0 {
		return &DeniedPathError{Path: trimmed}
	}
	if strings.ContainsRune(trimmed, 0) {
		return &DeniedPathError{Path: trimmed, Roots: p.Roots(), Reason: "the path contains a NUL byte"}
	}

	resolved, err := resolvePath(trimmed)
	if err != nil {
		return &DeniedPathError{Path: trimmed, Roots: p.Roots(), Reason: err.Error()}
	}

	for _, root := range p.roots {
		if pathWithin(root, resolved) {
			return nil
		}
	}

	return &DeniedPathError{Path: trimmed, Roots: p.Roots()}
}

// DeniedPathError explains a rejected path and names the only command that can
// grant access.
type DeniedPathError struct {
	Path   string
	Roots  []string
	Reason string
}

// Error implements error.
func (e *DeniedPathError) Error() string {
	if e == nil {
		return "path is not allowed"
	}

	if e.Reason != "" {
		return fmt.Sprintf(
			"path %q is not allowed: %s. Allowed roots: %s. Grant a folder from the terminal with: %s",
			e.Path, e.Reason, describeRoots(e.Roots), grantCommand,
		)
	}
	if len(e.Roots) == 0 {
		return fmt.Sprintf(
			"path %q is not allowed: no allowed roots are configured. Grant a folder from the terminal with: %s",
			e.Path, grantCommand,
		)
	}

	return fmt.Sprintf(
		"path %q is outside the allowed roots (%s). Grant another folder from the terminal with: %s",
		e.Path, strings.Join(e.Roots, ", "), grantCommand,
	)
}

func describeRoots(roots []string) string {
	if len(roots) == 0 {
		return "none"
	}

	return strings.Join(roots, ", ")
}

// canonicalRoot resolves a stored root best-effort. A grant whose directory is
// temporarily unavailable keeps its absolute lexical form so a future remount
// still matches.
func canonicalRoot(root string) string {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return ""
	}

	absolute, err := filepath.Abs(trimmed)
	if err != nil {
		return ""
	}
	absolute = filepath.Clean(absolute)
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		return filepath.Clean(resolved)
	}

	return absolute
}

// resolvePath returns the symlink-free form of path. When the full path does
// not exist (an output directory that will be created by the run), the deepest
// existing ancestor is resolved and the missing tail is re-appended. A dangling
// symbolic link in that tail is refused instead of being treated as a literal
// name, because it could point outside a granted root once the target appears.
func resolvePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(absolute)

	if resolved, resolveErr := filepath.EvalSymlinks(clean); resolveErr == nil {
		return resolved, nil
	} else if !errors.Is(resolveErr, fs.ErrNotExist) {
		return "", resolveErr
	}

	suffix := make([]string, 0, 4)
	current := clean
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return clean, nil
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent

		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Join(parts...), nil
		}
		if !errors.Is(resolveErr, fs.ErrNotExist) {
			return "", resolveErr
		}
		if info, lstatErr := os.Lstat(current); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path component %q is a symbolic link that cannot be resolved", current)
		}
	}
}

// pathWithin reports whether target is base or lives below it. Both inputs must
// already be absolute and resolved.
func pathWithin(base, target string) bool {
	baseKey := fsx.HostPathKey(base)
	targetKey := fsx.HostPathKey(target)
	if baseKey == "" || targetKey == "" {
		return false
	}

	relative, err := filepath.Rel(baseKey, targetKey)
	if err != nil {
		return false
	}

	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
