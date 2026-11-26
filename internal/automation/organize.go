package automation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/fsx"
)

const (
	// organizeDirPermissions matches the rest of the file stores.
	organizeDirPermissions = 0o755
	// organizeFilePermissions is the fallback mode when a source has no mode.
	organizeFilePermissions = 0o644

	// maxOrganizeSuffixAttempts bounds the -2, -3, ... collision loop.
	maxOrganizeSuffixAttempts = 1000

	// organizeHistoryToolID labels the synthetic history entries an organize
	// move creates, so the undo panel can tell them apart from tool jobs.
	organizeHistoryToolID = "automation.organize"
)

// organizePlanner resolves destinations for one rule. The watcher and the
// preview both build it, so template, collision and containment logic cannot
// drift between the dry run and execution.
type organizePlanner struct {
	service        *Service
	rule           models.AutomationRule
	targetDir      string
	folderTemplate string
	nameTemplate   string
	collision      string
	// reserved holds the destination keys already claimed during this planning
	// pass, so two files planned together never resolve to the same path.
	reserved map[string]struct{}
}

func newOrganizePlanner(service *Service, rule models.AutomationRule) (*organizePlanner, error) {
	targetDir := strings.TrimSpace(rule.TargetDir)
	if targetDir == "" {
		return nil, fmt.Errorf("The rule has no target folder.")
	}
	if !filepath.IsAbs(targetDir) {
		return nil, fmt.Errorf("The target folder must be an absolute path.")
	}
	targetDir = filepath.Clean(targetDir)

	folderTemplate := strings.TrimSpace(rule.FolderTemplate)
	if err := validateOrganizeTemplates(folderTemplate, rule.NameTemplate); err != nil {
		return nil, err
	}

	return &organizePlanner{
		service:        service,
		rule:           rule,
		targetDir:      targetDir,
		folderTemplate: folderTemplate,
		nameTemplate:   strings.TrimSpace(rule.NameTemplate),
		collision:      normalizeCollisionPolicy(rule.CollisionPolicy),
		reserved:       make(map[string]struct{}),
	}, nil
}

// planPath resolves the move for one file. It only reads: statistics, metadata
// and directory probes. The returned action is OrganizeMoveAction or
// OrganizeSkipAction with a reason; the destination is the path execution will
// use.
func (p *organizePlanner) planPath(path string, info os.FileInfo) models.OrganizePlannedMove {
	move := models.OrganizePlannedMove{
		Source:    path,
		Action:    models.OrganizeSkipAction,
		SizeBytes: info.Size(),
	}

	title := ""
	if p.service != nil {
		title = p.service.organizeTitle(path)
	}
	meta := organizeMetaFor(path, info.ModTime(), title)

	destination, err := renderOrganizeDestination(p.targetDir, p.folderTemplate, p.nameTemplate, meta)
	if err != nil {
		move.Reason = err.Error()

		return move
	}

	if fsx.SameFile(path, destination) || fsx.HostPathKey(path) == fsx.HostPathKey(destination) {
		move.Reason = "The file is already at its destination."

		return move
	}
	if !pathWithin(p.targetDir, destination) {
		move.Reason = "The rendered destination is outside the target folder."

		return move
	}

	resolved, reason := p.resolveCollision(destination)
	if reason != "" {
		move.Reason = reason

		return move
	}

	p.reserved[fsx.HostPathKey(resolved)] = struct{}{}
	move.Action = models.OrganizeMoveAction
	move.Destination = resolved

	return move
}

// resolveCollision applies the rule's collision policy to a desired path. The
// skipped reason is human-readable and is what the preview shows.
func (p *organizePlanner) resolveCollision(desired string) (string, string) {
	if !p.occupied(desired) {
		return desired, ""
	}
	if p.collision != models.AutomationCollisionSuffix {
		return "", "The destination already exists, so the file was left in place."
	}

	extension := filepath.Ext(desired)
	base := strings.TrimSuffix(desired, extension)
	for suffix := 2; suffix <= maxOrganizeSuffixAttempts; suffix++ {
		candidate := fmt.Sprintf("%s-%d%s", base, suffix, extension)
		if !p.occupied(candidate) {
			return candidate, ""
		}
	}

	return "", "The destination already exists and no free name was found, so the file was left in place."
}

func (p *organizePlanner) occupied(path string) bool {
	if _, claimed := p.reserved[fsx.HostPathKey(path)]; claimed {
		return true
	}

	return fsx.Exists(path)
}

// normalizeCollisionPolicy returns the effective policy; an empty value is the
// documented skip default.
func normalizeCollisionPolicy(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == models.AutomationCollisionSuffix {
		return models.AutomationCollisionSuffix
	}

	return models.AutomationCollisionSkip
}

// organizeTitle resolves {title} through the service seam. A nil title function
// (or a format without metadata) returns an empty string and the template
// falls back to {name}.
func (s *Service) organizeTitle(path string) string {
	if s == nil || s.titleFor == nil {
		return ""
	}

	return s.titleFor(path)
}

// planOrganizeMoves scans the watch folder and returns the full preview plan.
// It never writes a file, never creates a folder and never records history.
func (s *Service) planOrganizeMoves(rule models.AutomationRule) ([]models.OrganizePlannedMove, *models.JobError) {
	planner, err := newOrganizePlanner(s, rule)
	if err != nil {
		return nil, automationError("AUTOMATION_RULE_ORGANIZE_INVALID", err.Error())
	}

	files, err := collectOrganizeCandidates(rule)
	if err != nil {
		return nil, automationError(
			"AUTOMATION_RULE_WATCH_UNREADABLE",
			fmt.Sprintf("The watch folder could not be read: %v", err),
		)
	}

	moves := make([]models.OrganizePlannedMove, 0, len(files))
	for _, path := range files {
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			continue
		}
		moves = append(moves, planner.planPath(path, info))
	}

	return moves, nil
}

// collectOrganizeCandidates lists every file the rule would accept, in a
// deterministic order, using the same filter the watcher applies.
func collectOrganizeCandidates(rule models.AutomationRule) ([]string, error) {
	filter := newRuleFilter(rule)
	files := make([]string, 0, 16)
	root := rule.WatchDir

	if !rule.Recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if _, ok := filter.acceptsPath(path); ok {
				files = append(files, path)
			}
		}

		return files, nil
	}

	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if filter.skipsDirectory(path) {
				return filepath.SkipDir
			}

			return nil
		}
		if _, ok := filter.acceptsPath(path); ok {
			files = append(files, path)
		}

		return nil
	})

	return files, walkErr
}

// organizeConfigHash fingerprints the fields that decide where a file goes. Any
// change to a relevant field produces a different hash and invalidates the
// stored preview.
func organizeConfigHash(rule models.AutomationRule) string {
	key := struct {
		Version         int      `json:"version"`
		Action          string   `json:"action"`
		WatchDir        string   `json:"watchDir"`
		Extensions      []string `json:"extensions"`
		Recursive       bool     `json:"recursive"`
		Patterns        []string `json:"patterns"`
		TargetDir       string   `json:"targetDir"`
		FolderTemplate  string   `json:"folderTemplate"`
		NameTemplate    string   `json:"nameTemplate"`
		CollisionPolicy string   `json:"collisionPolicy"`
	}{
		Version:         1,
		Action:          rule.Action,
		WatchDir:        fsx.HostPathKey(rule.WatchDir),
		Extensions:      sortedCopy(rule.Extensions),
		Recursive:       rule.Recursive,
		Patterns:        sortedCopy(rule.Patterns),
		TargetDir:       fsx.HostPathKey(rule.TargetDir),
		FolderTemplate:  strings.TrimSpace(rule.FolderTemplate),
		NameTemplate:    strings.TrimSpace(rule.NameTemplate),
		CollisionPolicy: normalizeCollisionPolicy(rule.CollisionPolicy),
	}

	payload, err := json.Marshal(key)
	if err != nil {
		// The key is a fixed struct of strings and bools; encoding cannot fail.
		return ""
	}
	sum := sha256.Sum256(payload)

	return hex.EncodeToString(sum[:])
}

func sortedCopy(values []string) []string {
	copied := append([]string{}, values...)
	sort.Strings(copied)

	return copied
}

// organizePreviewCurrent reports whether the stored preview matches the current
// configuration. It is the server-side enable gate.
func organizePreviewCurrent(rule models.AutomationRule) bool {
	if rule.Action != models.AutomationActionOrganize {
		return true
	}
	if strings.TrimSpace(rule.PreviewHash) == "" {
		return false
	}

	return rule.PreviewHash == organizeConfigHash(rule)
}

// recordOrganizeMove records one successful move in the shared undo history so
// the existing Undo/Restore UI can revert it unchanged.
func (s *Service) recordOrganizeMove(rule models.AutomationRule, source, destination string) {
	if s == nil || s.moveHistory == nil {
		return
	}

	jobID := fmt.Sprintf("automation_%s_%d", rule.ID, time.Now().UnixNano())
	s.moveHistory.RecordMoves(jobID, organizeHistoryToolID, []jobhistory.MoveRecord{{
		Source:      source,
		Destination: destination,
	}})
}

// organizeMover is the filesystem seam for organize moves. Tests replace the
// fields to exercise the cross-volume and verification failure paths without a
// second real volume.
type organizeMover struct {
	rename      func(oldPath, newPath string) error
	copyFile    func(src, dst string) error
	verifyCopy  func(src, dst string) error
	remove      func(path string) error
	crossVolume func(src, dst string) bool
}

func defaultOrganizeMover() organizeMover {
	return organizeMover{
		rename:      os.Rename,
		copyFile:    copyOrganizeFile,
		verifyCopy:  verifyOrganizeCopy,
		remove:      os.Remove,
		crossVolume: differentVolume,
	}
}

// differentVolume reports whether src and dst live on different volumes. On
// POSIX both volume names are empty and a rename is attempted first, which is
// the fast path; Windows compares drive letters.
func differentVolume(src, dst string) bool {
	return !strings.EqualFold(filepath.VolumeName(src), filepath.VolumeName(dst))
}

// moveOrganizedFile implements the safety order for a move: rename on the same
// volume, otherwise copy, verify size and content hash, then remove the source.
// The source is never removed before its copy is verified, and a failed verify
// removes the partial copy and leaves the source untouched.
func (s *Service) moveOrganizedFile(source, destination string) error {
	mover := defaultOrganizeMover()
	if s != nil {
		mover = s.mover
	}
	if mover.rename == nil {
		mover.rename = os.Rename
	}
	if mover.copyFile == nil {
		mover.copyFile = copyOrganizeFile
	}
	if mover.verifyCopy == nil {
		mover.verifyCopy = verifyOrganizeCopy
	}
	if mover.remove == nil {
		mover.remove = os.Remove
	}
	if mover.crossVolume == nil {
		mover.crossVolume = differentVolume
	}

	if err := fsx.EnsureDir(filepath.Dir(destination), organizeDirPermissions); err != nil {
		return fmt.Errorf("The destination folder could not be created: %w", err)
	}

	// The planner resolved a free path; re-checking immediately before the move
	// keeps a file that appeared in the meantime from being overwritten.
	if fsx.Exists(destination) {
		return fmt.Errorf("The destination appeared while the rule was running, so the file was left in place: %s", destination)
	}

	if !mover.crossVolume(source, destination) {
		if err := mover.rename(source, destination); err == nil {
			return nil
		}
		// A rename can fail on Windows when the destination is briefly locked,
		// and on any platform when the volumes differ. Fall through to the
		// copy-and-verify path instead of failing the file.
	}

	if err := mover.copyFile(source, destination); err != nil {
		// The copy helper only removes a destination it created itself. A
		// failed create (for example because a file appeared at the resolved
		// path) must never delete the file that won the race.
		return fmt.Errorf("The file could not be copied, so it was left in place: %w", err)
	}

	copiedFingerprint, statErr := fingerprintFile(destination)
	if statErr != nil {
		return fmt.Errorf("The copied file could not be inspected, so the source was left in place: %w", statErr)
	}
	if err := mover.verifyCopy(source, destination); err != nil {
		// Remove only the copy this run made; if the destination changed
		// identity meanwhile it belongs to someone else and stays untouched.
		if current, currentErr := fingerprintFile(destination); currentErr == nil && current == copiedFingerprint {
			_ = mover.remove(destination)
		}

		return fmt.Errorf("The copied file could not be verified, so the source was left in place: %w", err)
	}
	if err := mover.remove(source); err != nil {
		// The verified copy is kept: removing it would discard the only
		// complete copy made by this run. The source is still in place.
		return fmt.Errorf("The file was copied but the source could not be removed: %w", err)
	}

	return nil
}

// copyOrganizeFile copies a regular file without ever overwriting the
// destination, preserving permissions and modification time so fingerprints
// recorded before the move stay valid.
func copyOrganizeFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file")
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	permissions := info.Mode().Perm()
	if permissions == 0 {
		permissions = organizeFilePermissions
	}
	target, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, permissions)
	if err != nil {
		return err
	}

	if _, copyErr := io.Copy(target, source); copyErr != nil {
		_ = target.Close()
		_ = os.Remove(dst)

		return copyErr
	}
	if err := target.Sync(); err != nil {
		_ = target.Close()
		_ = os.Remove(dst)

		return err
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(dst)

		return err
	}

	modified := info.ModTime()
	if err := os.Chtimes(dst, modified, modified); err != nil {
		_ = os.Remove(dst)

		return err
	}

	return nil
}

// verifyOrganizeCopy proves the destination is a faithful copy: the size must
// match and both files must hash to the same SHA-256 digest.
func verifyOrganizeCopy(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		return err
	}
	if srcInfo.Size() != dstInfo.Size() {
		return fmt.Errorf("size differs: source %d bytes, destination %d bytes", srcInfo.Size(), dstInfo.Size())
	}

	srcHash, err := hashFileContents(src)
	if err != nil {
		return err
	}
	dstHash, err := hashFileContents(dst)
	if err != nil {
		return err
	}
	if srcHash != dstHash {
		return fmt.Errorf("content hash differs")
	}

	return nil
}

func hashFileContents(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
