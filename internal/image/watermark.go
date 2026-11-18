package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"strings"
	"sync"
	"unicode"

	"github.com/disintegration/imaging"
	"github.com/fogleman/gg"
	"github.com/h2non/bimg"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
	"golang.org/x/text/unicode/norm"
)

const (
	watermarkKindText = "text"
	watermarkKindLogo = "logo"

	watermarkDefaultAnchor = "bottom-right"
	watermarkDefaultColor  = "#FFFFFF"

	watermarkMinMarginPercent     = 2.0
	watermarkMaxMarginPercent     = 5.0
	watermarkMinTextScalePercent  = 3.0
	watermarkMaxTextScalePercent  = 8.0
	watermarkMinLogoScalePercent  = 5.0
	watermarkMaxLogoScalePercent  = 60.0
	watermarkMinOpacity           = 0.05
	watermarkMaxOpacity           = 1.0
	watermarkMinRotationDegrees   = -90.0
	watermarkMaxRotationDegrees   = 90.0
	watermarkMaxTextRunes         = 200
	watermarkMaxTextLines         = 10
	watermarkDefaultQuality       = 92
	watermarkMinQuality           = 1
	watermarkMaxQuality           = 100
	watermarkMinFontSize          = 12
	watermarkTextLineHeightFactor = 1.25
)

// watermarkAnchorNames lists every supported anchor in UI order.
var watermarkAnchorNames = []string{
	"top-left", "top-center", "top-right",
	"center-left", "center", "center-right",
	"bottom-left", "bottom-center", "bottom-right",
}

// WatermarkOptions describes one text or logo watermark pass.
//
// Format accepts jpeg, png or webp. Quality defaults to 92 and only affects
// JPEG and WebP; PNG is encoded losslessly with maximum compression. Background
// is only used when the caller set it: bimg skips flattening when Background is
// its zero value (black), so a white flatten for alpha JPEG output must be set
// explicitly.
type WatermarkOptions struct {
	Kind            string      // "text" | "logo"
	Text            string      // text kind: may contain \n, max 10 lines
	Bold            bool        // text kind: bundled bold face
	Color           string      // #RRGGBB
	Logo            []byte      // logo kind: encoded PNG/JPEG/WebP bytes
	Anchor          string      // top-left|top-center|...|bottom-right
	MarginPercent   float64     // 2..5 of the short edge
	ScalePercent    float64     // text 3..8 (font size %), logo 5..60 (width %)
	Opacity         float64     // 0.05..1
	Tile            bool        // text only
	RotationDegrees float64     // -90..90, tile only
	Format          string      // jpeg|png|webp
	Quality         int         // jpeg/webp, default 92
	Background      *bimg.Color // optional, JPEG flatten
}

// WatermarkOutcome reports what the engine actually drew.
//
// Width and Height describe the encoded output. FontSize is zero for logo
// watermarks and LogoWidth is zero for text watermarks. MarginPixels is the
// inset used by the anchor box, in output pixels.
type WatermarkOutcome struct {
	Width        int
	Height       int
	Anchor       string
	FontSize     int
	MarginPixels int
	LogoWidth    int
}

// watermarkResolvedOptions holds the validated and normalized engine input.
type watermarkResolvedOptions struct {
	kind            string
	text            string
	logo            []byte
	bold            bool
	color           color.NRGBA
	anchor          string
	marginPercent   float64
	scalePercent    float64
	opacity         float64
	tile            bool
	rotationDegrees float64
	format          string
	imageType       bimg.ImageType
	quality         int
	background      *bimg.Color
}

// watermarkTextBlock is the measured layout of a multi-line text watermark.
type watermarkTextBlock struct {
	lines      []string
	width      float64
	height     float64
	ascent     float64
	lineHeight float64
}

// ApplyWatermark draws a text or logo watermark and re-encodes the image.
//
// EXIF orientations 2..8 are applied physically first (via a PNG round trip
// that also strips the tag), so mirrored and rotated photos are drawn on the
// displayed axes and the final encode never carries a stale orientation. Text
// uses the bundled Go fonts; logos are downscaled with Lanczos and never
// upscaled beyond the source or the image width. Tiled text renders one small
// stamp and blits it on a staggered grid, never allocating a full-size overlay.
// The output is always JPEG, PNG or WebP with metadata stripped.
func ApplyWatermark(input []byte, opts WatermarkOptions) ([]byte, WatermarkOutcome, error) {
	resolved, resolveErr := watermarkResolveOptions(opts)
	if resolveErr != nil {
		return nil, WatermarkOutcome{}, resolveErr
	}

	normalized, normalizeErr := watermarkNormalizeOrientation(input)
	if normalizeErr != nil {
		return nil, WatermarkOutcome{}, normalizeErr
	}

	decoded, _, decodeErr := image.Decode(bytes.NewReader(normalized))
	if decodeErr != nil {
		return nil, WatermarkOutcome{}, fmt.Errorf("decode image: %w", decodeErr)
	}

	width := decoded.Bounds().Dx()
	height := decoded.Bounds().Dy()
	if width < 1 || height < 1 {
		return nil, WatermarkOutcome{}, fmt.Errorf("decode image: invalid dimensions %dx%d", width, height)
	}

	gc := gg.NewContextForImage(imaging.Clone(decoded))

	outcome := WatermarkOutcome{
		Anchor: resolved.anchor,
	}
	shortEdge := width
	if height < shortEdge {
		shortEdge = height
	}
	outcome.MarginPixels = int(math.Max(1, math.Round(float64(shortEdge)*resolved.marginPercent/100)))

	if resolved.kind == watermarkKindLogo {
		logoWidth, logoErr := watermarkDrawLogo(gc, resolved, width, height, outcome.MarginPixels)
		if logoErr != nil {
			return nil, WatermarkOutcome{}, logoErr
		}
		outcome.LogoWidth = logoWidth
	} else {
		requestedFontSize := watermarkFontSize(shortEdge, resolved.scalePercent)
		usedFontSize, drawErr := watermarkDrawText(gc, resolved, width, height, requestedFontSize, outcome.MarginPixels)
		if drawErr != nil {
			return nil, WatermarkOutcome{}, drawErr
		}
		// Auto-fit and the tile width cap may shrink the requested size; report
		// the size that was actually drawn.
		outcome.FontSize = usedFontSize
	}

	encoded, encodeErr := watermarkEncode(gc.Image(), resolved)
	if encodeErr != nil {
		return nil, WatermarkOutcome{}, encodeErr
	}

	size, sizeErr := bimg.NewImage(encoded).Size()
	if sizeErr != nil {
		return nil, WatermarkOutcome{}, fmt.Errorf("read watermarked image size: %w", sizeErr)
	}
	outcome.Width = size.Width
	outcome.Height = size.Height

	return encoded, outcome, nil
}

// watermarkResolveOptions normalizes defaults and rejects anything outside the
// documented ranges before a single pixel is drawn.
func watermarkResolveOptions(opts WatermarkOptions) (watermarkResolvedOptions, error) {
	resolved := watermarkResolvedOptions{
		kind:            strings.ToLower(strings.TrimSpace(opts.Kind)),
		text:            watermarkNormalizeText(opts.Text),
		logo:            opts.Logo,
		bold:            opts.Bold,
		anchor:          strings.ToLower(strings.TrimSpace(opts.Anchor)),
		marginPercent:   opts.MarginPercent,
		scalePercent:    opts.ScalePercent,
		opacity:         opts.Opacity,
		tile:            opts.Tile,
		rotationDegrees: opts.RotationDegrees,
		format:          normalizeFormatName(opts.Format),
		quality:         opts.Quality,
		background:      opts.Background,
	}

	if resolved.kind == "" {
		resolved.kind = watermarkKindText
	}
	if resolved.kind != watermarkKindText && resolved.kind != watermarkKindLogo {
		return watermarkResolvedOptions{}, fmt.Errorf("kind must be text or logo (got %q)", opts.Kind)
	}

	if resolved.anchor == "" {
		resolved.anchor = watermarkDefaultAnchor
	}
	if _, _, ok := watermarkAnchorFactors(resolved.anchor); !ok {
		return watermarkResolvedOptions{}, fmt.Errorf("anchor is invalid: %q", opts.Anchor)
	}

	switch resolved.kind {
	case watermarkKindText:
		if err := watermarkValidateText(resolved.text); err != nil {
			return watermarkResolvedOptions{}, err
		}
		face, faceErr := watermarkFontFace(resolved.bold, watermarkMinFontSize)
		if faceErr != nil {
			return watermarkResolvedOptions{}, faceErr
		}
		if unsupported, ok := watermarkUnsupportedRune(face, resolved.text); ok {
			return watermarkResolvedOptions{}, watermarkUnsupportedRuneError(unsupported)
		}
		resolved.logo = nil
	case watermarkKindLogo:
		if len(opts.Logo) == 0 {
			return watermarkResolvedOptions{}, fmt.Errorf("logo bytes are required for logo watermarks")
		}
		if opts.Tile {
			return watermarkResolvedOptions{}, fmt.Errorf("tile is only supported for text watermarks")
		}
		resolved.text = ""
	}

	if !watermarkInRange(resolved.marginPercent, watermarkMinMarginPercent, watermarkMaxMarginPercent) {
		return watermarkResolvedOptions{}, fmt.Errorf("marginPercent must be between 2 and 5 (got %v)", opts.MarginPercent)
	}

	scaleMin, scaleMax := watermarkMinTextScalePercent, watermarkMaxTextScalePercent
	if resolved.kind == watermarkKindLogo {
		scaleMin, scaleMax = watermarkMinLogoScalePercent, watermarkMaxLogoScalePercent
	}
	if !watermarkInRange(resolved.scalePercent, scaleMin, scaleMax) {
		return watermarkResolvedOptions{}, fmt.Errorf("scalePercent must be between %v and %v for %s watermarks (got %v)", scaleMin, scaleMax, resolved.kind, opts.ScalePercent)
	}

	if !watermarkInRange(resolved.opacity, watermarkMinOpacity, watermarkMaxOpacity) {
		return watermarkResolvedOptions{}, fmt.Errorf("opacity must be between 0.05 and 1 (got %v)", opts.Opacity)
	}

	if !watermarkInRange(resolved.rotationDegrees, watermarkMinRotationDegrees, watermarkMaxRotationDegrees) {
		return watermarkResolvedOptions{}, fmt.Errorf("rotationDegrees must be between -90 and 90 (got %v)", opts.RotationDegrees)
	}

	colorValue := strings.TrimSpace(opts.Color)
	if colorValue == "" {
		colorValue = watermarkDefaultColor
	}
	parsedColor, colorErr := watermarkParseColor(colorValue, resolved.opacity)
	if colorErr != nil {
		return watermarkResolvedOptions{}, colorErr
	}
	resolved.color = parsedColor

	if resolved.format == "" {
		return watermarkResolvedOptions{}, fmt.Errorf("format is required: use jpeg, png or webp")
	}
	switch resolved.format {
	case "jpeg", "png", "webp":
	default:
		return watermarkResolvedOptions{}, fmt.Errorf("format must be jpeg, png or webp (got %q)", opts.Format)
	}
	resolved.imageType = mapFormatToImageType(resolved.format)
	if !bimg.IsTypeSupportedSave(resolved.imageType) {
		return watermarkResolvedOptions{}, fmt.Errorf("format %s is not supported by this libvips build", resolved.format)
	}

	if resolved.quality <= 0 {
		resolved.quality = watermarkDefaultQuality
	}
	if resolved.quality < watermarkMinQuality || resolved.quality > watermarkMaxQuality {
		return watermarkResolvedOptions{}, fmt.Errorf("quality must be between 1 and 100 (got %d)", opts.Quality)
	}

	return resolved, nil
}

// watermarkValidateText enforces the text surface limits: non-empty, bounded
// characters and lines, and no control characters other than the newline that
// separates lines.
func watermarkValidateText(text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("text is required for text watermarks")
	}
	if len([]rune(text)) > watermarkMaxTextRunes {
		return fmt.Errorf("text must be at most %d characters", watermarkMaxTextRunes)
	}
	if len(strings.Split(text, "\n")) > watermarkMaxTextLines {
		return fmt.Errorf("text must be at most %d lines", watermarkMaxTextLines)
	}
	for _, character := range text {
		if character != '\n' && (character < 0x20 || character == 0x7f) {
			return fmt.Errorf("text must not contain control characters")
		}
	}

	return nil
}

// watermarkNormalizeText folds Windows line endings and lone carriage returns
// into \n and applies NFC so decomposed accents compose into the single rune
// the bundled font contains.
func watermarkNormalizeText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	return norm.NFC.String(text)
}

// watermarkUnsupportedRune returns the first non-space rune the face cannot
// draw. The bundled Go fonts cover Latin, Greek and Cyrillic only.
func watermarkUnsupportedRune(face font.Face, text string) (rune, bool) {
	for _, character := range text {
		if unicode.IsSpace(character) {
			continue
		}
		if _, _, ok := face.GlyphBounds(character); !ok {
			return character, true
		}
	}

	return 0, false
}

// watermarkUnsupportedRuneError names the first missing glyph and suggests a
// logo watermark instead of promising coverage the bundled font may not have.
func watermarkUnsupportedRuneError(character rune) error {
	return fmt.Errorf(
		"text contains a character the bundled font does not include: %q (U+%04X); use a logo watermark for scripts or characters the bundled font lacks",
		character,
		character,
	)
}

// watermarkParseColor accepts only #RRGGBB and applies the requested opacity.
// parseColorWithOpacity silently falls back to red on invalid input, so the
// strict check happens here first.
func watermarkParseColor(value string, opacity float64) (color.NRGBA, error) {
	trimmed := strings.TrimSpace(value)
	if !watermarkValidHexColor(trimmed) {
		return color.NRGBA{}, fmt.Errorf("color must be a #RRGGBB hex value (got %q)", value)
	}

	return parseColorWithOpacity(trimmed, opacity)
}

func watermarkValidHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	for _, character := range value[1:] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return false
		}
	}

	return true
}

// watermarkInRange rejects NaN and infinities, which would slip through plain
// comparisons and land in the encoder.
func watermarkInRange(value, min, max float64) bool {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}

	return value >= min && value <= max
}

// watermarkAnchorFactors maps an anchor to its 0, 0.5 or 1 factors.
func watermarkAnchorFactors(anchor string) (float64, float64, bool) {
	switch anchor {
	case "top-left":
		return 0, 0, true
	case "top-center":
		return 0.5, 0, true
	case "top-right":
		return 1, 0, true
	case "center-left":
		return 0, 0.5, true
	case "center":
		return 0.5, 0.5, true
	case "center-right":
		return 1, 0.5, true
	case "bottom-left":
		return 0, 1, true
	case "bottom-center":
		return 0.5, 1, true
	case "bottom-right":
		return 1, 1, true
	default:
		return 0, 0, false
	}
}

// watermarkFontSize derives the pixel size from the short edge. The result is
// clamped to a 12 px legibility floor and, when the short edge is larger than
// that floor, capped at the short edge so a font can never exceed it.
func watermarkFontSize(shortEdge int, scalePercent float64) int {
	size := int(math.Round(float64(shortEdge) * scalePercent / 100))
	if size < watermarkMinFontSize {
		size = watermarkMinFontSize
	}
	upper := shortEdge
	if upper < watermarkMinFontSize {
		upper = watermarkMinFontSize
	}
	if size > upper {
		size = upper
	}

	return size
}

var (
	watermarkRegularFontOnce sync.Once
	watermarkRegularFont     *opentype.Font
	watermarkRegularFontErr  error
	watermarkBoldFontOnce    sync.Once
	watermarkBoldFont        *opentype.Font
	watermarkBoldFontErr     error
)

// watermarkCachedFont parses each bundled TTF once; batch items reuse the
// parsed sfnt instead of reparsing the font per image.
func watermarkCachedFont(bold bool) (*opentype.Font, error) {
	if bold {
		watermarkBoldFontOnce.Do(func() {
			watermarkBoldFont, watermarkBoldFontErr = opentype.Parse(gobold.TTF)
		})

		return watermarkBoldFont, watermarkBoldFontErr
	}

	watermarkRegularFontOnce.Do(func() {
		watermarkRegularFont, watermarkRegularFontErr = opentype.Parse(goregular.TTF)
	})

	return watermarkRegularFont, watermarkRegularFontErr
}

// watermarkFontFace builds one face from the cached bundled font. Preferring the
// embedded fonts keeps rendering identical on every platform.
func watermarkFontFace(bold bool, size int) (font.Face, error) {
	parsed, parseErr := watermarkCachedFont(bold)
	if parseErr != nil {
		return nil, fmt.Errorf("parse font: %w", parseErr)
	}

	face, faceErr := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(size), DPI: 72})
	if faceErr != nil {
		return nil, fmt.Errorf("create font face: %w", faceErr)
	}

	return face, nil
}

// watermarkMeasureText lays out the lines left aligned. The block height is the
// baseline span plus ascent and descent, so anchored placement is exact.
func watermarkMeasureText(text string, face font.Face, fontSize int) watermarkTextBlock {
	metrics := face.Metrics()
	ascent := float64(metrics.Ascent) / 64
	descent := float64(metrics.Descent) / 64
	lineHeight := float64(fontSize) * watermarkTextLineHeightFactor
	lines := strings.Split(text, "\n")

	width := 0.0
	for _, line := range lines {
		lineWidth := float64(font.MeasureString(face, line)) / 64
		if lineWidth > width {
			width = lineWidth
		}
	}

	return watermarkTextBlock{
		lines:      lines,
		width:      width,
		height:     float64(len(lines)-1)*lineHeight + ascent + descent,
		ascent:     ascent,
		lineHeight: lineHeight,
	}
}

// watermarkDrawText places the measured block against the nine-anchor inset
// box, or renders a tiled stamp when tile mode is on. It returns the font size
// actually drawn: the block is shrunk until it fits both inset dimensions, and
// it errors instead of clipping when even the 12 px floor cannot fit.
func watermarkDrawText(gc *gg.Context, opts watermarkResolvedOptions, width, height, fontSize, margin int) (int, error) {
	if opts.tile {
		return watermarkDrawTiledText(gc, opts, fontSize)
	}

	maxWidth := float64(width - 2*margin)
	maxHeight := float64(height - 2*margin)
	if maxWidth <= 0 || maxHeight <= 0 {
		return 0, fmt.Errorf("the margin leaves no room for a text watermark")
	}

	usedSize := fontSize
	var face font.Face
	var block watermarkTextBlock
	for {
		faceAtSize, faceErr := watermarkFontFace(opts.bold, usedSize)
		if faceErr != nil {
			return 0, faceErr
		}
		face = faceAtSize
		block = watermarkMeasureText(opts.text, face, usedSize)
		if block.width <= maxWidth && block.height <= maxHeight {
			break
		}
		if usedSize <= watermarkMinFontSize {
			return 0, fmt.Errorf(
				"text does not fit the image at the minimum font size (%d px); use fewer or shorter lines, or a larger image",
				watermarkMinFontSize,
			)
		}

		widthRatio := 1.0
		if block.width > 0 {
			widthRatio = maxWidth / block.width
		}
		heightRatio := maxHeight / block.height
		next := int(math.Floor(float64(usedSize) * math.Min(widthRatio, heightRatio)))
		if next >= usedSize {
			next = usedSize - 1
		}
		if next < watermarkMinFontSize {
			next = watermarkMinFontSize
		}
		usedSize = next
	}

	ax, ay, _ := watermarkAnchorFactors(opts.anchor)
	anchorX := float64(margin) + float64(width-2*margin)*ax
	anchorY := float64(margin) + float64(height-2*margin)*ay
	left := anchorX - ax*block.width
	top := anchorY - ay*block.height

	gc.SetFontFace(face)
	gc.SetColor(opts.color)
	for index, line := range block.lines {
		gc.DrawString(line, left, top+block.ascent+float64(index)*block.lineHeight)
	}

	return usedSize, nil
}

// watermarkTilePadding keeps a visual gap around the tiled text block.
func watermarkTilePadding(fontSize int) float64 {
	padding := float64(fontSize / 2)
	if padding < 2 {
		padding = 2
	}

	return padding
}

// watermarkAlphaBounds returns the bounding box of every pixel with non-zero
// alpha, i.e. the visible ink rather than the font ascent/descent box.
func watermarkAlphaBounds(img image.Image) image.Rectangle {
	switch typed := img.(type) {
	case *image.RGBA:
		return watermarkPixAlphaBounds(typed.Pix, typed.Stride, typed.Bounds())
	case *image.NRGBA:
		return watermarkPixAlphaBounds(typed.Pix, typed.Stride, typed.Bounds())
	}

	bounds := img.Bounds()
	changed := image.Rectangle{}
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if pixel.A == 0 {
				continue
			}
			pixelRect := image.Rect(x, y, x+1, y+1)
			if !found {
				changed = pixelRect
				found = true
				continue
			}
			changed = changed.Union(pixelRect)
		}
	}

	return changed
}

func watermarkPixAlphaBounds(pix []byte, stride int, bounds image.Rectangle) image.Rectangle {
	changed := image.Rectangle{}
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := (y - bounds.Min.Y) * stride
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if pix[row+(x-bounds.Min.X)*4+3] == 0 {
				continue
			}
			pixelRect := image.Rect(x, y, x+1, y+1)
			if !found {
				changed = pixelRect
				found = true
				continue
			}
			changed = changed.Union(pixelRect)
		}
	}

	return changed
}

// watermarkRenderInk renders the text block on an opaque white mask and crops
// to the visible ink, so the glyph's actual bounding box drives the stamp
// instead of the font ascent/descent metrics.
func watermarkRenderInk(text string, face font.Face, fontSize int) (*image.RGBA, error) {
	block := watermarkMeasureText(text, face, fontSize)
	margin := float64(fontSize) * 2
	canvasWidth := int(math.Ceil(block.width + 2*margin))
	canvasHeight := int(math.Ceil(block.height + 2*margin))
	if canvasWidth < 1 {
		canvasWidth = 1
	}
	if canvasHeight < 1 {
		canvasHeight = 1
	}

	canvas := gg.NewContext(canvasWidth, canvasHeight)
	canvas.SetFontFace(face)
	canvas.SetColor(color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	for index, line := range block.lines {
		canvas.DrawString(line, margin, margin+block.ascent+float64(index)*block.lineHeight)
	}

	rendered, ok := canvas.Image().(*image.RGBA)
	if !ok {
		return nil, fmt.Errorf("render text ink: unexpected image type")
	}

	inkBounds := watermarkAlphaBounds(rendered)
	if inkBounds.Empty() {
		return nil, fmt.Errorf("text produced no visible glyphs")
	}

	cropped, ok := rendered.SubImage(inkBounds).(*image.RGBA)
	if !ok {
		return nil, fmt.Errorf("crop text ink: unexpected image type")
	}

	return cropped, nil
}

// watermarkRenderTileStamp renders the ink-centered tiled stamp with symmetric
// padding. The coverage mask comes from a white render, so the stamp size is
// independent of the requested color and opacity.
func watermarkRenderTileStamp(text string, bold bool, fontSize int, fill color.NRGBA) (*image.NRGBA, error) {
	face, faceErr := watermarkFontFace(bold, fontSize)
	if faceErr != nil {
		return nil, faceErr
	}

	ink, inkErr := watermarkRenderInk(text, face, fontSize)
	if inkErr != nil {
		return nil, inkErr
	}

	padding := int(watermarkTilePadding(fontSize))
	inkBounds := ink.Bounds()
	stamp := image.NewNRGBA(image.Rect(0, 0, inkBounds.Dx()+2*padding, inkBounds.Dy()+2*padding))
	for y := inkBounds.Min.Y; y < inkBounds.Max.Y; y++ {
		for x := inkBounds.Min.X; x < inkBounds.Max.X; x++ {
			source := ink.RGBAAt(x, y)
			if source.A == 0 {
				continue
			}
			alpha := uint8(math.Round(float64(source.A) * float64(fill.A) / 255))
			if alpha == 0 {
				continue
			}
			stamp.SetNRGBA(
				padding+x-inkBounds.Min.X,
				padding+y-inkBounds.Min.Y,
				color.NRGBA{R: fill.R, G: fill.G, B: fill.B, A: alpha},
			)
		}
	}

	return stamp, nil
}

// watermarkDrawTiledText renders one small ink-centered stamp, optionally
// rotates it, and blits it on a centered staggered grid. The canvas is never
// copied into a full-size overlay, and the font shrinks until the stamp fits
// the canvas width; a stamp still too wide at the 12 px floor is a clear error
// instead of a huge allocation.
func watermarkDrawTiledText(gc *gg.Context, opts watermarkResolvedOptions, fontSize int) (int, error) {
	canvasWidth := gc.Width()
	canvasHeight := gc.Height()

	// Pick the font size from the advance-based estimate first so an oversized
	// request never allocates a huge stamp just to discover it must shrink.
	usedSize := fontSize
	for {
		face, faceErr := watermarkFontFace(opts.bold, usedSize)
		if faceErr != nil {
			return 0, faceErr
		}
		block := watermarkMeasureText(opts.text, face, usedSize)
		padding := watermarkTilePadding(usedSize)
		estimate := int(math.Ceil(block.width + 2*padding))
		if estimate <= canvasWidth {
			break
		}
		if usedSize <= watermarkMinFontSize {
			return 0, fmt.Errorf(
				"text is too wide to tile at the minimum font size (%d px); shorten the text or use a larger image",
				watermarkMinFontSize,
			)
		}

		next := int(math.Floor(float64(usedSize) * float64(canvasWidth) / float64(estimate)))
		if next >= usedSize {
			next = usedSize - 1
		}
		if next < watermarkMinFontSize {
			next = watermarkMinFontSize
		}
		usedSize = next
	}

	stamp, stampErr := watermarkRenderTileStamp(opts.text, opts.bold, usedSize, opts.color)
	if stampErr != nil {
		return 0, stampErr
	}
	// Glyph overhang can push the actual ink slightly past the advance estimate.
	for stamp.Bounds().Dx() > canvasWidth {
		if usedSize <= watermarkMinFontSize {
			return 0, fmt.Errorf(
				"text is too wide to tile at the minimum font size (%d px); shorten the text or use a larger image",
				watermarkMinFontSize,
			)
		}
		usedSize--
		stamp, stampErr = watermarkRenderTileStamp(opts.text, opts.bold, usedSize, opts.color)
		if stampErr != nil {
			return 0, stampErr
		}
	}

	var rotated image.Image = stamp
	if opts.rotationDegrees != 0 {
		rotated = imaging.Rotate(stamp, opts.rotationDegrees, color.Transparent)
	}

	stampWidth := rotated.Bounds().Dx()
	stampHeight := rotated.Bounds().Dy()
	if stampWidth < 1 || stampHeight < 1 {
		return usedSize, nil
	}

	// Spacing is derived from the rotated stamp so the text keeps a visual gap.
	gapX := stampWidth / 2
	if gapX < 2 {
		gapX = 2
	}
	gapY := stampHeight / 2
	if gapY < 2 {
		gapY = 2
	}
	stepX := stampWidth + gapX
	stepY := stampHeight + gapY

	inkBounds := watermarkAlphaBounds(rotated)
	if inkBounds.Empty() {
		return usedSize, nil
	}
	inkTop := inkBounds.Min.Y
	inkHeight := inkBounds.Dy()

	// Center the sequence of visible ink bands, not the stamp boxes: a whole
	// stamp can sit above the canvas while its padded box is still centered.
	rows := 1
	if inkHeight < canvasHeight {
		rows = int(math.Floor(float64(canvasHeight-inkHeight)/float64(stepY))) + 1
	}
	if rows < 1 {
		rows = 1
	}
	span := (rows-1)*stepY + inkHeight
	// Adding one row avoids leaving a border wider than roughly one font size;
	// when the span exceeds the canvas the ink is clipped symmetrically instead.
	if float64(canvasHeight-span) > 2*float64(usedSize) {
		rows++
		span += stepY
	}
	startY := int(math.Round(float64(canvasHeight-span)/2)) - inkTop

	for row := 0; row < rows; row++ {
		y := startY + row*stepY
		startX := -stampWidth
		if row%2 != 0 {
			startX -= stepX / 2
		}
		for x := startX; x < canvasWidth; x += stepX {
			gc.DrawImage(rotated, x, y)
		}
	}

	return usedSize, nil
}

// watermarkDrawLogo normalizes the logo's EXIF orientation, downscales it with
// Lanczos, applies the opacity on the NRGBA alpha channel (no premultiplication
// bugs) and draws it against the selected anchor. It returns the drawn logo
// width in pixels.
func watermarkDrawLogo(gc *gg.Context, opts watermarkResolvedOptions, width, height, margin int) (int, error) {
	normalizedLogo, normalizeErr := watermarkNormalizeOrientation(opts.logo)
	if normalizeErr != nil {
		return 0, fmt.Errorf("normalize logo orientation: %w", normalizeErr)
	}

	decodedLogo, _, decodeErr := image.Decode(bytes.NewReader(normalizedLogo))
	if decodeErr != nil {
		return 0, fmt.Errorf("decode logo: %w", decodeErr)
	}

	logoBounds := decodedLogo.Bounds()
	logoWidth := logoBounds.Dx()
	logoHeight := logoBounds.Dy()
	if logoWidth < 1 || logoHeight < 1 {
		return 0, fmt.Errorf("decode logo: invalid dimensions %dx%d", logoWidth, logoHeight)
	}

	targetWidth := int(math.Round(float64(width) * opts.scalePercent / 100))
	if targetWidth < 1 {
		targetWidth = 1
	}
	if targetWidth > logoWidth {
		targetWidth = logoWidth
	}
	if targetWidth > width {
		targetWidth = width
	}

	var scaled *image.NRGBA
	if targetWidth < logoWidth {
		scaled = imaging.Resize(decodedLogo, targetWidth, 0, imaging.Lanczos)
	} else {
		scaled = imaging.Clone(decodedLogo)
	}
	if scaled.Bounds().Dx() < 1 || scaled.Bounds().Dy() < 1 {
		return 0, fmt.Errorf("resize logo failed: invalid dimensions")
	}

	watermarkApplyOpacity(scaled, opts.opacity)

	ax, ay, _ := watermarkAnchorFactors(opts.anchor)
	anchorX := float64(margin) + float64(width-2*margin)*ax
	anchorY := float64(margin) + float64(height-2*margin)*ay
	gc.DrawImageAnchored(scaled, int(math.Round(anchorX)), int(math.Round(anchorY)), ax, ay)

	return scaled.Bounds().Dx(), nil
}

// watermarkApplyOpacity multiplies the alpha channel in place.
func watermarkApplyOpacity(img *image.NRGBA, opacity float64) {
	if opacity >= 1 {
		return
	}

	for index := 3; index < len(img.Pix); index += 4 {
		img.Pix[index] = uint8(math.Round(float64(img.Pix[index]) * opacity))
	}
}

// watermarkEncode routes the drawn RGBA canvas through bimg with an explicit
// output type. Metadata is always stripped because the pixels are already
// orientation-normalized; a kept tag would rotate the output twice.
func watermarkEncode(canvas image.Image, opts watermarkResolvedOptions) ([]byte, error) {
	var intermediate bytes.Buffer
	if err := png.Encode(&intermediate, canvas); err != nil {
		return nil, fmt.Errorf("encode intermediate png: %w", err)
	}

	encodeOptions := bimg.Options{
		Type:          opts.imageType,
		StripMetadata: true,
		Enlarge:       false,
	}
	switch opts.imageType {
	case bimg.JPEG:
		encodeOptions.Quality = opts.quality
		encodeOptions.Interlace = true
	case bimg.WEBP:
		encodeOptions.Quality = opts.quality
	case bimg.PNG:
		encodeOptions.Compression = 9
	}
	if opts.background != nil {
		encodeOptions.Background = *opts.background
	}

	encoded, encodeErr := bimg.NewImage(intermediate.Bytes()).Process(encodeOptions)
	if encodeErr != nil {
		return nil, fmt.Errorf("encode %s: %w", opts.format, encodeErr)
	}

	return encoded, nil
}

// watermarkNormalizeOrientation physically applies EXIF orientations 2..8. The
// PNG round trip mirrors and rotates the pixels the same way a viewer would and
// strips the stale tag; orientation 0/1 decodes as stored. Applying the full
// range matters for 2..4 too: stripping the tag without mirroring would
// silently lose the flip.
func watermarkNormalizeOrientation(input []byte) ([]byte, error) {
	metadata, metaErr := bimg.NewImage(input).Metadata()
	if metaErr != nil {
		// Let image.Decode produce the user-facing error for unreadable input.
		return input, nil
	}
	if metadata.Orientation < 2 || metadata.Orientation > 8 {
		return input, nil
	}

	rotated, rotateErr := bimg.NewImage(input).Process(bimg.Options{
		Type:          bimg.PNG,
		StripMetadata: true,
	})
	if rotateErr != nil {
		return nil, fmt.Errorf("normalize exif orientation: %w", rotateErr)
	}

	return rotated, nil
}
