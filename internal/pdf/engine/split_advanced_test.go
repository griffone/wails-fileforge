package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
)

func writeMultiPageTestPDF(t *testing.T, path string, pages int) {
	t.Helper()

	pngPath := filepath.Join(t.TempDir(), "page.png")
	writeTestPNG(t, pngPath)

	images := make([]string, 0, pages)
	for i := 0; i < pages; i++ {
		images = append(images, pngPath)
	}

	if err := api.ImportImagesFile(context.Background(), images, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

func writeBookmarkedTestPDF(t *testing.T, path string, pages int, bookmarks []pdfcpu.Bookmark) {
	t.Helper()

	plain := filepath.Join(t.TempDir(), "plain.pdf")
	writeMultiPageTestPDF(t, plain, pages)

	if err := api.AddBookmarksFile(context.Background(), plain, path, bookmarks, true, nil); err != nil {
		t.Fatalf("add bookmarks: %v", err)
	}
}

func TestSplitAdvancedPagesPerFilePlan(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "manual.pdf")
	outputDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	writeMultiPageTestPDF(t, input, 11)

	outputs, err := SplitAdvanced(context.Background(), input, outputDir, SplitStrategyPagesPerFile, "", 5)
	if err != nil {
		t.Fatalf("SplitAdvanced returned error: %v", err)
	}
	if len(outputs) != 3 {
		t.Fatalf("expected 3 chunk outputs, got %d: %v", len(outputs), outputs)
	}

	wantNames := []string{
		"manual_part_001_p1-5.pdf",
		"manual_part_002_p6-10.pdf",
		"manual_part_003_p11.pdf",
	}
	wantPages := []int{5, 5, 1}
	for i, output := range outputs {
		if want := filepath.Join(outputDir, wantNames[i]); output != want {
			t.Fatalf("expected output %d %q, got %q", i, want, output)
		}

		count, countErr := api.PageCountFile(context.Background(), output)
		if countErr != nil {
			t.Fatalf("page count of %q: %v", output, countErr)
		}
		if count != wantPages[i] {
			t.Fatalf("expected output %d to have %d pages, got %d", i, wantPages[i], count)
		}
	}

	// A second pass must fail instead of overwriting the first run.
	if _, secondErr := SplitAdvanced(context.Background(), input, outputDir, SplitStrategyPagesPerFile, "", 5); secondErr == nil {
		t.Fatal("expected a second run to fail on existing outputs")
	}
}

func TestSplitAdvancedPagesPerFileValidation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "manual.pdf")
	writeMultiPageTestPDF(t, input, 3)

	cases := []struct {
		pagesPerFile int
		wantValid    bool
	}{
		{pagesPerFile: -1},
		{pagesPerFile: 0},
		{pagesPerFile: 1001},
		{pagesPerFile: 1, wantValid: true},
		{pagesPerFile: 1000, wantValid: true},
	}
	for _, testCase := range cases {
		t.Run(fmt.Sprintf("pagesPerFile=%d", testCase.pagesPerFile), func(t *testing.T) {
			outputDir := filepath.Join(dir, fmt.Sprintf("out-%d", testCase.pagesPerFile))
			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				t.Fatalf("create output dir: %v", err)
			}

			_, err := SplitAdvanced(context.Background(), input, outputDir, SplitStrategyPagesPerFile, "", testCase.pagesPerFile)
			if testCase.wantValid {
				if err != nil {
					t.Fatalf("expected pagesPerFile=%d to pass validation, got %v", testCase.pagesPerFile, err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected pagesPerFile=%d to fail", testCase.pagesPerFile)
			}
			if !IsSplitErrorCode(err, ErrorCodeSplitPagesPerFileInvalid) {
				t.Fatalf("expected %s for pagesPerFile=%d, got %v", ErrorCodeSplitPagesPerFileInvalid, testCase.pagesPerFile, err)
			}
		})
	}
}

func TestSplitAdvancedBookmarksWithoutOutline(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "plain.pdf")
	outputDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	writeMultiPageTestPDF(t, input, 4)

	outputs, err := SplitAdvanced(context.Background(), input, outputDir, SplitStrategyBookmarks, "", 0)
	if err == nil {
		t.Fatalf("expected a no-bookmarks error, got outputs %v", outputs)
	}
	if !IsSplitErrorCode(err, ErrorCodeSplitNoBookmarks) {
		t.Fatalf("expected %s, got %v", ErrorCodeSplitNoBookmarks, err)
	}

	if _, rangeErr := SplitBookmarkPageRanges(input); !IsSplitErrorCode(rangeErr, ErrorCodeSplitNoBookmarks) {
		t.Fatalf("expected %s from SplitBookmarkPageRanges, got %v", ErrorCodeSplitNoBookmarks, rangeErr)
	}
}

func TestSplitAdvancedBookmarksPlanAndExecute(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "guide.pdf")
	outputDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	writeBookmarkedTestPDF(t, input, 11, []pdfcpu.Bookmark{
		{Title: "Chapter One", PageFrom: 1},
		{Title: "Chapter Two", PageFrom: 6},
		{Title: "Appendix", PageFrom: 9},
	})

	outputs, err := SplitAdvanced(context.Background(), input, outputDir, SplitStrategyBookmarks, "", 0)
	if err != nil {
		t.Fatalf("SplitAdvanced returned error: %v", err)
	}
	if len(outputs) != 3 {
		t.Fatalf("expected 3 bookmark outputs, got %d: %v", len(outputs), outputs)
	}

	wantNames := []string{
		"guide_bookmark_001_chapter-one.pdf",
		"guide_bookmark_002_chapter-two.pdf",
		"guide_bookmark_003_appendix.pdf",
	}
	wantPages := []int{5, 3, 3}
	for i, output := range outputs {
		if want := filepath.Join(outputDir, wantNames[i]); output != want {
			t.Fatalf("expected output %d %q, got %q", i, want, output)
		}

		count, countErr := api.PageCountFile(context.Background(), output)
		if countErr != nil {
			t.Fatalf("page count of %q: %v", output, countErr)
		}
		if count != wantPages[i] {
			t.Fatalf("expected output %d to have %d pages, got %d", i, wantPages[i], count)
		}
	}
}

func TestDeriveBookmarkRanges(t *testing.T) {
	ranges, splitErr := deriveBookmarkRanges([]pdfcpu.Bookmark{
		{Title: "Intro", PageFrom: 1},
		{Title: "Body", PageFrom: 3},
		{Title: "Appendix", PageFrom: 8, PageThru: 9},
	}, 10)
	if splitErr != nil {
		t.Fatalf("expected ranges to derive, got %v", splitErr)
	}

	want := []SplitBookmarkRange{
		{Title: "Intro", Start: 1, End: 2},
		{Title: "Body", Start: 3, End: 7},
		{Title: "Appendix", Start: 8, End: 9},
	}
	if len(ranges) != len(want) {
		t.Fatalf("expected %d ranges, got %d: %+v", len(want), len(ranges), ranges)
	}
	for i := range want {
		if ranges[i] != want[i] {
			t.Fatalf("range %d: expected %+v, got %+v", i, want[i], ranges[i])
		}
	}

	lastRanges, lastErr := deriveBookmarkRanges([]pdfcpu.Bookmark{
		{Title: "Only", PageFrom: 4},
	}, 10)
	if lastErr != nil {
		t.Fatalf("expected single bookmark to derive, got %v", lastErr)
	}
	if len(lastRanges) != 1 || lastRanges[0].Start != 4 || lastRanges[0].End != 10 {
		t.Fatalf("expected last bookmark to reach the last page, got %+v", lastRanges)
	}
}

func TestDeriveBookmarkRangesSkipsUnusableBookmarks(t *testing.T) {
	ranges, splitErr := deriveBookmarkRanges([]pdfcpu.Bookmark{
		{Title: "Broken", PageFrom: 0},
		{Title: "Valid", PageFrom: 2},
	}, 5)
	if splitErr != nil {
		t.Fatalf("expected usable bookmark to survive, got %v", splitErr)
	}
	if len(ranges) != 1 || ranges[0].Title != "Valid" || ranges[0].Start != 2 || ranges[0].End != 5 {
		t.Fatalf("expected only the valid bookmark, got %+v", ranges)
	}

	if _, allBrokenErr := deriveBookmarkRanges([]pdfcpu.Bookmark{{Title: "Broken", PageFrom: 0}}, 5); !IsSplitErrorCode(allBrokenErr, ErrorCodeSplitNoBookmarks) {
		t.Fatalf("expected %s when no bookmark is usable, got %v", ErrorCodeSplitNoBookmarks, allBrokenErr)
	}

	if _, outOfBoundsErr := deriveBookmarkRanges([]pdfcpu.Bookmark{{Title: "Far", PageFrom: 9}}, 5); !IsSplitErrorCode(outOfBoundsErr, ErrorCodeSplitBookmarksInvalid) {
		t.Fatalf("expected %s for an out-of-bounds bookmark, got %v", ErrorCodeSplitBookmarksInvalid, outOfBoundsErr)
	}

	if _, emptyErr := deriveBookmarkRanges(nil, 5); !IsSplitErrorCode(emptyErr, ErrorCodeSplitNoBookmarks) {
		t.Fatalf("expected %s for no bookmarks, got %v", ErrorCodeSplitNoBookmarks, emptyErr)
	}
}

func TestBuildBookmarksPlanKeepsNamesUnique(t *testing.T) {
	planned := buildBookmarksPlan("/tmp/report.pdf", "/tmp/out", []SplitBookmarkRange{
		{Title: "Intro", Start: 1, End: 2},
		{Title: "Intro", Start: 3, End: 4},
		{Title: "", Start: 5, End: 5},
	})

	// The output index keeps same-title bookmarks distinct.
	wantNames := []string{
		"report_bookmark_001_intro.pdf",
		"report_bookmark_002_intro.pdf",
		"report_bookmark_003_part.pdf",
	}
	if len(planned) != len(wantNames) {
		t.Fatalf("expected %d planned outputs, got %d", len(wantNames), len(planned))
	}
	for i, entry := range planned {
		if want := filepath.Join("/tmp/out", wantNames[i]); entry.Output != want {
			t.Fatalf("expected output %d %q, got %q", i, want, entry.Output)
		}
	}
}

func TestBookmarkSlug(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{title: "Chapter One", want: "chapter-one"},
		{title: "  Intro & Setup  ", want: "intro-setup"},
		{title: "A  B", want: "a-b"},
		{title: "", want: "part"},
		{title: "!!!", want: "part"},
		{title: strings.Repeat("abcdefghij", 5), want: "abcdefghijabcdefghijabcdefghijabcdefghij"},
		{title: "Name_With_Underscore", want: "name_with_underscore"},
	}

	for _, test := range tests {
		if got := bookmarkSlug(test.title); got != test.want {
			t.Fatalf("bookmarkSlug(%q): expected %q, got %q", test.title, test.want, got)
		}
	}
}
