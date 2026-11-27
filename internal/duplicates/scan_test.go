package duplicates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/testutil"
)

func TestScanGroupsIdenticalContentAcrossNamesAndTimes(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("fileforge"), 1024)

	alpha := filepath.Join(directory, "alpha.txt")
	renamed := filepath.Join(directory, "renamed copy.bin")
	unique := filepath.Join(directory, "unique.txt")
	testutil.WriteFile(t, alpha, content)
	testutil.WriteFile(t, renamed, content)
	testutil.WriteFile(t, unique, []byte("a different file with a different size"))

	firstTime := time.Unix(1_600_000_000, 0)
	secondTime := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(alpha, firstTime, firstTime); err != nil {
		t.Fatalf("set alpha mtime: %v", err)
	}
	if err := os.Chtimes(renamed, secondTime, secondTime); err != nil {
		t.Fatalf("set renamed mtime: %v", err)
	}

	result, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	}, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(result.Groups) != 1 {
		t.Fatalf("groups = %d, want 1: %+v", len(result.Groups), result.Groups)
	}
	group := result.Groups[0]
	if group.Count != 2 {
		t.Fatalf("group count = %d, want 2", group.Count)
	}
	if group.SizeBytes != int64(len(content)) {
		t.Fatalf("group size = %d, want %d", group.SizeBytes, len(content))
	}
	if group.WastedBytes != int64(len(content)) {
		t.Fatalf("wasted = %d, want %d", group.WastedBytes, len(content))
	}
	wantPaths := []string{alpha, renamed}
	sort.Strings(wantPaths)
	if fmt.Sprint(group.Paths) != fmt.Sprint(wantPaths) {
		t.Fatalf("paths = %v, want %v", group.Paths, wantPaths)
	}
	if result.Totals.FilesScanned != 3 {
		t.Fatalf("files scanned = %d, want 3", result.Totals.FilesScanned)
	}
	if result.Totals.FilesHashed != 2 {
		t.Fatalf("files hashed = %d, want 2", result.Totals.FilesHashed)
	}
}

func TestScanDoesNotGroupSameSizeDifferentContent(t *testing.T) {
	directory := t.TempDir()
	testutil.WriteFile(t, filepath.Join(directory, "first.bin"), bytes.Repeat([]byte("A"), 2048))
	testutil.WriteFile(t, filepath.Join(directory, "second.bin"), bytes.Repeat([]byte("B"), 2048))

	result, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	}, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(result.Groups) != 0 {
		t.Fatalf("groups = %d, want 0: %+v", len(result.Groups), result.Groups)
	}
	if result.Totals.FilesHashed != 2 {
		t.Fatalf("files hashed = %d, want 2", result.Totals.FilesHashed)
	}
}

func TestScanNeverHashesUniqueSizeFiles(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("same"), 1024)
	testutil.WriteFile(t, filepath.Join(directory, "one.bin"), content)
	testutil.WriteFile(t, filepath.Join(directory, "two.bin"), content)
	testutil.WriteFile(t, filepath.Join(directory, "lonely.bin"), bytes.Repeat([]byte("z"), 4097))

	result, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	}, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if result.Totals.FilesScanned != 3 {
		t.Fatalf("files scanned = %d, want 3", result.Totals.FilesScanned)
	}
	if result.Totals.FilesHashed != 2 {
		t.Fatalf("files hashed = %d, want 2 (the unique-size file must never be hashed)", result.Totals.FilesHashed)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(result.Groups))
	}
}

func TestScanSkipsTinyFilesBelowMinimum(t *testing.T) {
	directory := t.TempDir()
	testutil.WriteFile(t, filepath.Join(directory, "tiny-a.txt"), []byte("abc"))
	testutil.WriteFile(t, filepath.Join(directory, "tiny-b.txt"), []byte("abc"))
	testutil.WriteFile(t, filepath.Join(directory, "empty-a.txt"), nil)
	testutil.WriteFile(t, filepath.Join(directory, "empty-b.txt"), nil)

	result, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories: []string{directory},
	}, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if result.Totals.FilesScanned != 0 {
		t.Fatalf("files scanned = %d, want 0 (default minimum skips tiny and empty files)", result.Totals.FilesScanned)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("groups = %d, want 0", len(result.Groups))
	}
}

func TestScanRespectsExtensionFilter(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("duplicate"), 1000)
	testutil.WriteFile(t, filepath.Join(directory, "note-a.txt"), content)
	testutil.WriteFile(t, filepath.Join(directory, "note-b.txt"), content)
	testutil.WriteFile(t, filepath.Join(directory, "log-a.log"), content)
	testutil.WriteFile(t, filepath.Join(directory, "log-b.log"), content)

	for _, extension := range []string{".txt", "TXT", ".TXT"} {
		t.Run(extension, func(t *testing.T) {
			result, err := Scan(context.Background(), models.DuplicateScanRequest{
				Directories:  []string{directory},
				MinSizeBytes: 1,
				Extensions:   []string{extension},
			}, nil)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}

			if result.Totals.FilesScanned != 2 || result.Totals.FilesHashed != 2 {
				t.Fatalf("scanned/hashed = %d/%d, want 2/2", result.Totals.FilesScanned, result.Totals.FilesHashed)
			}
			if len(result.Groups) != 1 {
				t.Fatalf("groups = %d, want 1", len(result.Groups))
			}
			for _, path := range result.Groups[0].Paths {
				if strings.ToLower(filepath.Ext(path)) != ".txt" {
					t.Fatalf("unexpected path %q for the txt filter", path)
				}
			}
		})
	}
}

func TestScanWarnsOnUnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions behave differently on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read a mode 000 file")
	}

	directory := t.TempDir()
	content := bytes.Repeat([]byte("readable"), 512)
	readable := filepath.Join(directory, "readable.bin")
	locked := filepath.Join(directory, "locked.bin")
	testutil.WriteFile(t, readable, content)
	testutil.WriteFile(t, locked, content)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod locked file: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(locked, 0o644)
	})

	result, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	}, nil)
	if err != nil {
		t.Fatalf("scan must not fail on an unreadable file: %v", err)
	}

	found := false
	for _, warning := range result.Warnings {
		if warning.Path == locked {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v, want one for %s", result.Warnings, locked)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("groups = %d, want 0 (one readable file cannot be a duplicate)", len(result.Groups))
	}
	if result.Totals.FilesHashed != 2 {
		t.Fatalf("files hashed = %d, want 2 attempts", result.Totals.FilesHashed)
	}
}

func TestScanDoesNotFollowSymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs extra privileges on Windows")
	}

	directory := t.TempDir()
	outside := t.TempDir()
	content := bytes.Repeat([]byte("linked"), 1024)
	first := filepath.Join(directory, "first.bin")
	second := filepath.Join(directory, "second.bin")
	testutil.WriteFile(t, first, content)
	testutil.WriteFile(t, second, content)
	testutil.WriteFile(t, filepath.Join(outside, "copy.bin"), content)

	link := filepath.Join(directory, "linked-folder")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	result, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	}, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(result.Groups) != 1 || result.Groups[0].Count != 2 {
		t.Fatalf("groups = %+v, want one group of 2", result.Groups)
	}
	for _, path := range result.Groups[0].Paths {
		if strings.HasPrefix(path, link) {
			t.Fatalf("scan followed the symlinked directory: %s", path)
		}
	}
	if result.Totals.FilesScanned != 2 {
		t.Fatalf("files scanned = %d, want 2", result.Totals.FilesScanned)
	}
}

func TestScanSkipsHiddenAndTempFiles(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("hidden"), 1024)
	testutil.WriteFile(t, filepath.Join(directory, "visible-a.bin"), content)
	testutil.WriteFile(t, filepath.Join(directory, "visible-b.bin"), content)
	testutil.WriteFile(t, filepath.Join(directory, ".hidden-copy.bin"), content)
	testutil.WriteFile(t, filepath.Join(directory, "download.bin.part"), content)
	testutil.WriteFile(t, filepath.Join(directory, "~$draft.bin"), content)

	result, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	}, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if result.Totals.FilesScanned != 2 {
		t.Fatalf("files scanned = %d, want 2", result.Totals.FilesScanned)
	}
	if len(result.Groups) != 1 || result.Groups[0].Count != 2 {
		t.Fatalf("groups = %+v, want one group of 2", result.Groups)
	}
}

func TestScanRecursiveDefaultAndOptOut(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("nested"), 1024)
	testutil.WriteFile(t, filepath.Join(directory, "top.bin"), content)
	testutil.WriteFile(t, filepath.Join(directory, "nested", "deeper.bin"), content)

	recursive, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	}, nil)
	if err != nil {
		t.Fatalf("recursive scan: %v", err)
	}
	if len(recursive.Groups) != 1 || recursive.Groups[0].Count != 2 {
		t.Fatalf("recursive groups = %+v, want one group of 2", recursive.Groups)
	}

	disabled := false
	flat, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
		Recursive:    &disabled,
	}, nil)
	if err != nil {
		t.Fatalf("flat scan: %v", err)
	}
	if len(flat.Groups) != 0 {
		t.Fatalf("flat groups = %+v, want none", flat.Groups)
	}
	if flat.Totals.FilesScanned != 1 {
		t.Fatalf("flat files scanned = %d, want 1", flat.Totals.FilesScanned)
	}
}

func TestScanFileCapStopsWalkingAndWarns(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("cap"), 1024)
	for index := 0; index < 5; index++ {
		testutil.WriteFile(t, filepath.Join(directory, fmt.Sprintf("file-%02d.bin", index)), content)
	}
	testutil.WriteFile(t, filepath.Join(directory, "zzz", "late.bin"), content)

	result, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
		MaxFiles:     3,
	}, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if result.Totals.FilesScanned != 3 {
		t.Fatalf("files scanned = %d, want the cap of 3", result.Totals.FilesScanned)
	}
	foundCapWarning := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning.Message, "file cap") {
			foundCapWarning = true
		}
	}
	if !foundCapWarning {
		t.Fatalf("warnings = %+v, want a file cap warning", result.Warnings)
	}
}

func TestScanCancellationReturnsPromptly(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("cancel-me"), 16*1024)
	for index := 0; index < 24; index++ {
		testutil.WriteFile(t, filepath.Join(directory, fmt.Sprintf("file-%02d.bin", index)), content)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := time.Now()
	_, err := Scan(ctx, models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	}, func(progress models.JobProgress) {
		if progress.Stage == StageHashing {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %s, want it to return promptly", elapsed)
	}
}

func TestScanOrderingIsDeterministic(t *testing.T) {
	directory := t.TempDir()
	small := bytes.Repeat([]byte("s"), 1024)
	medium := bytes.Repeat([]byte("m"), 2048)
	large := bytes.Repeat([]byte("l"), 4096)

	testutil.WriteFile(t, filepath.Join(directory, "b", "large-1.bin"), large)
	testutil.WriteFile(t, filepath.Join(directory, "a", "large-2.bin"), large)
	testutil.WriteFile(t, filepath.Join(directory, "b", "large-3.bin"), large)
	testutil.WriteFile(t, filepath.Join(directory, "a", "medium-1.bin"), medium)
	testutil.WriteFile(t, filepath.Join(directory, "medium-2.bin"), medium)
	testutil.WriteFile(t, filepath.Join(directory, "small-1.bin"), small)
	testutil.WriteFile(t, filepath.Join(directory, "small-2.bin"), small)

	first, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
		Workers:      4,
	}, nil)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	second, err := Scan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
		Workers:      2,
	}, nil)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("scan results are not deterministic:\nfirst:  %s\nsecond: %s", firstJSON, secondJSON)
	}

	if len(first.Groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(first.Groups))
	}
	for index, group := range first.Groups {
		if index > 0 && group.WastedBytes > first.Groups[index-1].WastedBytes {
			t.Fatalf("groups are not sorted by wasted bytes: %+v", first.Groups)
		}
		if !sort.StringsAreSorted(group.Paths) {
			t.Fatalf("group paths are not sorted: %v", group.Paths)
		}
	}
}

func TestValidateRequestRejectsBadFolders(t *testing.T) {
	if err := ValidateRequest(models.DuplicateScanRequest{}); err == nil {
		t.Fatal("expected an error for an empty request")
	}

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := ValidateRequest(models.DuplicateScanRequest{Directories: []string{missing}}); err == nil {
		t.Fatalf("expected an error for a missing folder %s", missing)
	}

	file := filepath.Join(t.TempDir(), "regular.txt")
	testutil.WriteFile(t, file, []byte("not a folder"))
	if err := ValidateRequest(models.DuplicateScanRequest{Directories: []string{file}}); err == nil {
		t.Fatalf("expected an error for a regular file %s", file)
	}
}
