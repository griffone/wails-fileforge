// Package duplicates finds groups of files with identical content. It is
// strictly read-only: nothing is deleted, moved, renamed or modified.
//
// A scan has two phases. Phase one walks the requested folders, applies the
// name, extension and size filters and buckets the surviving files by exact
// byte size. A file whose size is unique cannot have a content duplicate, so it
// is never read. Phase two hashes only the files that share a size with another
// candidate, streaming SHA-256 through a bounded worker pool, and groups the
// files whose hashes match.
package duplicates

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/fsx"
)

const (
	// DefaultWorkers is the hashing concurrency used when a request does not
	// choose one. Four workers keep a local disk busy without the random-seek
	// thrash that comes from one reader per core on a spinning drive.
	DefaultWorkers = 4
	// MaxWorkers bounds a caller-supplied worker count so a single scan cannot
	// spawn an unreasonable number of goroutines.
	MaxWorkers = 16
	// DefaultMaxFiles bounds one scan. Each retained file costs an entry in the
	// size map plus its path in the result, so the cap is what keeps memory
	// predictable on a very large folder.
	DefaultMaxFiles = 20_000
	// DefaultMinSizeBytes skips empty and tiny files. Hashing thousands of
	// multi-byte files wastes I/O and is almost never what the user means.
	DefaultMinSizeBytes int64 = 4 * 1024
	// hashBufferSize is the streaming read size. 32 KiB amortises syscalls
	// while staying small enough that a cancelled scan stops quickly.
	hashBufferSize = 32 * 1024

	// Progress stages reported while a scan runs.
	StageWalking = "walking"
	StageHashing = "hashing"
	StageDone    = "done"

	// progressWalkEvery and progressHashEvery throttle progress callbacks: a
	// 20k-file scan emits tens of events instead of one per file.
	progressWalkEvery = 256
	progressHashEvery = 64
)

// Result is the output of one scan. Every slice is non-nil so callers can
// serialize an empty result without nulls.
type Result struct {
	Groups   []models.DuplicateScanGroup
	Totals   models.DuplicateScanTotals
	Warnings []models.DuplicateScanWarning
}

// fileCandidate is one file that passed the walk filters.
type fileCandidate struct {
	path string
	size int64
}

// scanPlan is the validated, normalised form of a request.
type scanPlan struct {
	directories []string
	recursive   bool
	minSize     int64
	extensions  map[string]struct{}
	maxFiles    int
	workers     int
	warnings    []models.DuplicateScanWarning
}

// ValidateRequest resolves and validates a request without scanning anything.
// It lets a service reject a bad request synchronously instead of failing in
// the background.
func ValidateRequest(request models.DuplicateScanRequest) error {
	_, err := prepareRequest(request)
	return err
}

// Scan runs one read-only duplicate scan. Progress callbacks are optional and
// are invoked serially from the scanning goroutine, so a caller does not need
// its own lock around them.
//
// A cancelled context returns promptly with context.Canceled (or the deadline
// error) and a partial result. A folder that cannot be read is a warning, not a
// failure; only an invalid request or a cancellation stops the scan.
func Scan(ctx context.Context, request models.DuplicateScanRequest, onProgress func(models.JobProgress)) (Result, error) {
	result := Result{
		Groups:   []models.DuplicateScanGroup{},
		Warnings: []models.DuplicateScanWarning{},
	}
	if ctx == nil {
		ctx = context.Background()
	}

	plan, err := prepareRequest(request)
	if err != nil {
		return result, err
	}
	result.Warnings = append(result.Warnings, plan.warnings...)

	emit := func(progress models.JobProgress) {
		if onProgress != nil {
			onProgress(progress)
		}
	}

	emit(models.JobProgress{Stage: StageWalking, Message: "Scanning folders"})
	files, walkWarnings, err := walkFiles(ctx, plan, emit)
	result.Warnings = append(result.Warnings, walkWarnings...)
	result.Totals.FilesScanned = len(files)
	if err != nil {
		sortWarnings(result.Warnings)
		return result, err
	}

	candidates := duplicateSizeCandidates(files)
	emit(models.JobProgress{
		Stage:   StageHashing,
		Total:   len(candidates),
		Message: fmt.Sprintf("Hashing %d candidate files", len(candidates)),
	})

	grouped, hashed, hashWarnings, err := hashCandidates(ctx, candidates, plan.workers, emit)
	result.Totals.FilesHashed = hashed
	result.Warnings = append(result.Warnings, hashWarnings...)
	if err != nil {
		sortWarnings(result.Warnings)
		return result, err
	}

	result.Groups = buildGroups(grouped)
	result.Totals.Groups = len(result.Groups)
	for _, group := range result.Groups {
		result.Totals.WastedBytes += group.WastedBytes
	}
	sortWarnings(result.Warnings)

	emit(models.JobProgress{
		Current: result.Totals.FilesScanned,
		Total:   result.Totals.FilesScanned,
		Stage:   StageDone,
		Message: resultMessage(result.Totals),
	})

	return result, nil
}

func resultMessage(totals models.DuplicateScanTotals) string {
	if totals.Groups == 0 {
		return "No duplicate files found"
	}

	return fmt.Sprintf(
		"Found %d duplicate %s wasting %s",
		totals.Groups,
		pluralize("group", totals.Groups),
		formatBytes(totals.WastedBytes),
	)
}

// prepareRequest normalises the request and resolves every folder. Folders are
// resolved through symlinks once, so an explicitly requested symlinked folder
// works while symlinked folders discovered during the walk are still not
// followed.
func prepareRequest(request models.DuplicateScanRequest) (scanPlan, error) {
	plan := scanPlan{
		recursive:  true,
		minSize:    DefaultMinSizeBytes,
		maxFiles:   DefaultMaxFiles,
		workers:    DefaultWorkers,
		extensions: map[string]struct{}{},
	}

	if len(request.Directories) == 0 {
		return plan, errors.New("duplicates: at least one folder is required")
	}

	seenDirs := make(map[string]struct{}, len(request.Directories))
	for _, raw := range request.Directories {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		resolved, err := resolveDirectory(trimmed)
		if err != nil {
			return plan, err
		}
		key := fsx.HostPathKey(resolved)
		if _, exists := seenDirs[key]; exists {
			continue
		}
		seenDirs[key] = struct{}{}
		plan.directories = append(plan.directories, resolved)
	}
	if len(plan.directories) == 0 {
		return plan, errors.New("duplicates: at least one folder is required")
	}

	if request.Recursive != nil {
		plan.recursive = *request.Recursive
	}

	switch {
	case request.MinSizeBytes < 0:
		// A negative value means "only skip empty files".
		plan.minSize = 0
	case request.MinSizeBytes > 0:
		plan.minSize = request.MinSizeBytes
	}

	switch {
	case request.MaxFiles < 0:
		plan.maxFiles = 0 // unlimited
	case request.MaxFiles > 0:
		plan.maxFiles = request.MaxFiles
	}

	if request.Workers > 0 {
		plan.workers = request.Workers
		if plan.workers > MaxWorkers {
			plan.warnings = append(plan.warnings, models.DuplicateScanWarning{
				Message: fmt.Sprintf("hashing workers limited to %d", MaxWorkers),
			})
			plan.workers = MaxWorkers
		}
	}

	for _, raw := range request.Extensions {
		extension := normalizeExtension(raw)
		if extension == "" {
			continue
		}
		plan.extensions[extension] = struct{}{}
	}

	return plan, nil
}

// resolveDirectory makes a requested folder absolute and resolves symlinks in
// the path itself. A missing path or a regular file is an error: the user asked
// for a folder that does not exist.
func resolveDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("duplicates: resolve folder %q: %w", path, err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}

	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("duplicates: folder %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("duplicates: %q is not a folder", path)
	}

	return absolute, nil
}

// walkFiles is phase one: it records every eligible file once and warns instead
// of aborting when a path cannot be read.
func walkFiles(ctx context.Context, plan scanPlan, emit func(models.JobProgress)) ([]fileCandidate, []models.DuplicateScanWarning, error) {
	files := make([]fileCandidate, 0)
	warnings := make([]models.DuplicateScanWarning, 0)
	seen := make(map[string]struct{})
	capReached := false

	for _, root := range plan.directories {
		walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}

			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					warnings = append(warnings, models.DuplicateScanWarning{Path: path, Message: "the path changed during the scan"})
					if entry != nil && entry.IsDir() {
						return fs.SkipDir
					}

					return nil
				}
				warnings = append(warnings, models.DuplicateScanWarning{
					Path:    path,
					Message: fmt.Sprintf("could not inspect path: %v", err),
				})
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}

				return nil
			}

			if entry.IsDir() {
				if path == root {
					return nil
				}
				// A non-recursive scan only reads the requested folder itself.
				if !plan.recursive || isIgnoredName(entry.Name()) {
					return fs.SkipDir
				}

				return nil
			}

			if isIgnoredName(entry.Name()) {
				return nil
			}

			info, infoErr := entry.Info()
			if infoErr != nil {
				warnings = append(warnings, models.DuplicateScanWarning{
					Path:    path,
					Message: fmt.Sprintf("could not inspect file: %v", infoErr),
				})

				return nil
			}
			// Regular files only: this also excludes symlinks, so a symlinked
			// directory is never followed and a symlink cannot make one file
			// look like a duplicate of itself.
			if !info.Mode().IsRegular() {
				return nil
			}
			if !plan.acceptsExtension(entry.Name()) {
				return nil
			}
			if info.Size() <= 0 || info.Size() < plan.minSize {
				return nil
			}

			key := fsx.HostPathKey(path)
			if _, exists := seen[key]; exists {
				return nil
			}
			if plan.maxFiles > 0 && len(files) >= plan.maxFiles {
				capReached = true
				return fs.SkipAll
			}

			seen[key] = struct{}{}
			files = append(files, fileCandidate{path: path, size: info.Size()})
			if len(files)%progressWalkEvery == 0 {
				emit(models.JobProgress{
					Current: len(files),
					Stage:   StageWalking,
					Message: fmt.Sprintf("Scanned %d files", len(files)),
				})
			}

			return nil
		})

		if walkErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return files, warnings, ctxErr
			}
			if capReached {
				break
			}
			warnings = append(warnings, models.DuplicateScanWarning{
				Path:    root,
				Message: fmt.Sprintf("could not scan folder: %v", walkErr),
			})
		}
		if capReached {
			break
		}
	}

	if capReached {
		log.Printf("duplicates.scan.file_cap directories=%d limit=%d", len(plan.directories), plan.maxFiles)
		warnings = append(warnings, models.DuplicateScanWarning{
			Message: fmt.Sprintf("stopped after the file cap of %d files; some files were not scanned", plan.maxFiles),
		})
	}

	return files, warnings, nil
}

func (p scanPlan) acceptsExtension(name string) bool {
	if len(p.extensions) == 0 {
		return true
	}
	_, accepted := p.extensions[normalizeExtension(filepath.Ext(name))]

	return accepted
}

// duplicateSizeCandidates is the prefilter: only sizes shared by at least two
// files can contain content duplicates. The result is path-sorted so the work
// order is deterministic.
func duplicateSizeCandidates(files []fileCandidate) []fileCandidate {
	buckets := make(map[int64][]fileCandidate, len(files))
	for _, file := range files {
		buckets[file.size] = append(buckets[file.size], file)
	}

	candidates := make([]fileCandidate, 0)
	for _, bucket := range buckets {
		if len(bucket) < 2 {
			continue
		}
		candidates = append(candidates, bucket...)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].path < candidates[j].path
	})

	return candidates
}

// hashResult carries one finished (or failed) hash attempt.
type hashResult struct {
	candidate fileCandidate
	hash      string
	err       error
}

// hashCandidates is phase two. A small bounded pool streams every candidate
// exactly once; results are grouped by hash after all workers finish so the
// output order never depends on which worker won a race.
func hashCandidates(ctx context.Context, candidates []fileCandidate, workers int, emit func(models.JobProgress)) (map[string][]fileCandidate, int, []models.DuplicateScanWarning, error) {
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan fileCandidate)
	results := make(chan hashResult, workers)
	var workerGroup sync.WaitGroup

	for index := 0; index < workers; index++ {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for candidate := range jobs {
				if ctx.Err() != nil {
					return
				}
				hash, err := hashFile(ctx, candidate.path)
				select {
				case results <- hashResult{candidate: candidate, hash: hash, err: err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, candidate := range candidates {
			select {
			case jobs <- candidate:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		workerGroup.Wait()
		close(results)
	}()

	grouped := make(map[string][]fileCandidate, len(candidates)/2)
	warnings := make([]models.DuplicateScanWarning, 0)
	hashed := 0
	var ctxErr error

	for result := range results {
		hashed++
		if result.err != nil {
			if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
				if ctxErr == nil {
					ctxErr = result.err
				}
				continue
			}
			warnings = append(warnings, models.DuplicateScanWarning{
				Path:    result.candidate.path,
				Message: fmt.Sprintf("could not read file: %v", result.err),
			})
			continue
		}

		grouped[result.hash] = append(grouped[result.hash], result.candidate)
		if hashed%progressHashEvery == 0 {
			emit(models.JobProgress{
				Current: hashed,
				Total:   len(candidates),
				Stage:   StageHashing,
				Message: fmt.Sprintf("Hashed %d of %d files", hashed, len(candidates)),
			})
		}
	}

	if err := ctx.Err(); err != nil {
		ctxErr = err
	}

	return grouped, hashed, warnings, ctxErr
}

// hashFile streams one file through SHA-256. The context is checked per chunk
// so cancellation does not wait for a large file to finish.
func hashFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	hasher := sha256.New()
	buffer := make([]byte, hashBufferSize)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		read, readErr := file.Read(buffer)
		if read > 0 {
			if _, err := hasher.Write(buffer[:read]); err != nil {
				return "", err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// buildGroups turns hash buckets into reportable groups. Single-file buckets
// are dropped: a hash that nobody shares is not a duplicate.
func buildGroups(byHash map[string][]fileCandidate) []models.DuplicateScanGroup {
	groups := make([]models.DuplicateScanGroup, 0, len(byHash))
	for hash, bucket := range byHash {
		if len(bucket) < 2 {
			continue
		}

		paths := make([]string, 0, len(bucket))
		var size int64
		for _, file := range bucket {
			paths = append(paths, file.path)
			if file.size > size {
				size = file.size
			}
		}
		sort.Strings(paths)

		count := len(paths)
		groups = append(groups, models.DuplicateScanGroup{
			Hash:        hash,
			SizeBytes:   size,
			Count:       count,
			WastedBytes: size * int64(count-1),
			Paths:       paths,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].WastedBytes != groups[j].WastedBytes {
			return groups[i].WastedBytes > groups[j].WastedBytes
		}
		if groups[i].SizeBytes != groups[j].SizeBytes {
			return groups[i].SizeBytes > groups[j].SizeBytes
		}

		return groups[i].Hash < groups[j].Hash
	})

	return groups
}

// isIgnoredName mirrors the automation watcher's rule: hidden and in-progress
// names are never scanned, at any depth.
func isIgnoredName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return true
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$") {
		return true
	}
	if strings.HasSuffix(name, "~") {
		return true
	}

	lower := strings.ToLower(name)
	for _, suffix := range []string{".tmp", ".part", ".crdownload"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}

	return false
}

// normalizeExtension returns a lowercase extension without the leading dot.
// Empty or invalid values are ignored by the caller.
func normalizeExtension(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	trimmed = strings.TrimPrefix(trimmed, ".")
	if trimmed == "" || strings.ContainsAny(trimmed, `/\`) || trimmed == "." {
		return ""
	}

	return trimmed
}

// sortWarnings makes warnings deterministic regardless of worker order.
func sortWarnings(warnings []models.DuplicateScanWarning) {
	sort.SliceStable(warnings, func(i, j int) bool {
		if warnings[i].Path != warnings[j].Path {
			return warnings[i].Path < warnings[j].Path
		}

		return warnings[i].Message < warnings[j].Message
	})
}

func pluralize(word string, count int) string {
	if count == 1 {
		return word
	}

	return word + "s"
}

// formatBytes renders a byte count for a user-facing summary message.
func formatBytes(bytes int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	value := float64(bytes)
	index := 0
	for value >= 1024 && index < len(units)-1 {
		value /= 1024
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d B", bytes)
	}

	return fmt.Sprintf("%.1f %s", value, units[index])
}
