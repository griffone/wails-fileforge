package engine

import (
	"math"
	"testing"
)

func TestResolvePageSetupDefaults(t *testing.T) {
	setup, err := resolvePageSetup(RenderConfig{})
	if err != nil {
		t.Fatalf("expected default page setup to resolve, got %v", err)
	}

	if setup.PageSize != "A4" || setup.Orientation != "P" {
		t.Fatalf("expected A4 portrait, got %s/%s", setup.PageSize, setup.Orientation)
	}
	if setup.PageWidthMM != 210 || setup.PageHeightMM != 297 {
		t.Fatalf("expected A4 dimensions, got %.1fx%.1f", setup.PageWidthMM, setup.PageHeightMM)
	}
	if setup.MarginTopMM != 20 || setup.MarginRightMM != 15 || setup.MarginBottomMM != 18 || setup.MarginLeftMM != 15 {
		t.Fatalf("unexpected default margins: %+v", setup)
	}
}

func TestResolvePageSetupKeepsExplicitZeroMargin(t *testing.T) {
	setup, err := resolvePageSetup(RenderConfig{MarginTopMM: 0, MarginRightMM: 10, MarginBottomMM: 10, MarginLeftMM: 10})
	if err != nil {
		t.Fatalf("expected page setup to resolve, got %v", err)
	}
	if setup.MarginTopMM != 0 {
		t.Fatalf("expected explicit zero top margin to be kept, got %.2f", setup.MarginTopMM)
	}
}

func TestResolvePageSetupLetterLandscape(t *testing.T) {
	setup, err := resolvePageSetup(RenderConfig{PageSize: "letter", Orientation: "l"})
	if err != nil {
		t.Fatalf("expected page setup to resolve, got %v", err)
	}

	if setup.PageSize != "Letter" {
		t.Fatalf("expected normalized Letter, got %q", setup.PageSize)
	}
	if setup.Orientation != "L" {
		t.Fatalf("expected normalized L, got %q", setup.Orientation)
	}
	if setup.PageWidthMM != 279.4 || setup.PageHeightMM != 215.9 {
		t.Fatalf("expected swapped Letter dimensions, got %.1fx%.1f", setup.PageWidthMM, setup.PageHeightMM)
	}
}

func TestResolvePageSetupRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		cfg  RenderConfig
	}{
		{name: "page size", cfg: RenderConfig{PageSize: "A5"}},
		{name: "orientation", cfg: RenderConfig{Orientation: "diagonal"}},
		{name: "negative margin", cfg: RenderConfig{MarginLeftMM: -1}},
		{name: "non finite margin", cfg: RenderConfig{MarginBottomMM: math.NaN()}},
		{name: "margins exceed page width", cfg: RenderConfig{MarginLeftMM: 150, MarginRightMM: 150}},
		{name: "margins exceed page height", cfg: RenderConfig{MarginTopMM: 200, MarginBottomMM: 200}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolvePageSetup(tc.cfg); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
