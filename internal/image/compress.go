package image

import (
	"context"
	"fmt"
	"math"

	"github.com/h2non/bimg"
)

const (
	compressJPEGMinQuality  = 40
	compressWebPMinQuality  = 30
	compressMaxQuality      = 95
	compressQualitySteps    = 7
	compressMaxDownscale    = 3
	compressDownscaleFloor  = 320
	compressDownscaleSafety = 0.98
	compressLosslessQuality = 100
)

// CompressOptions controls the size-target search. A zero TargetSizeBytes is
// rejected; the remaining fields are optional.
//
// StripMetadata removes EXIF/GPS data when true. The tool resolves the product
// default (true) before calling the engine; when stripping would exceed the
// target or grow the file, the original bytes are kept and the outcome reports
// MetadataPreserved.
type CompressOptions struct {
	TargetSizeBytes int64
	Format          string
	AllowDownscale  bool
	MaxDimension    int
	Quality         int
	StripMetadata   bool
	Background      *bimg.Color
}

// CompressOutcome describes the image the engine selected.
//
// Width and Height describe the returned bytes: a copied original reports its
// stored pixel axes (the EXIF orientation tag is preserved, so the stored axes
// are what the file contains), while an encoded candidate reports its encoded
// pixel size. Quality is the encoder quality that produced the candidate (the
// palette quality for PNG, 100 for lossless PNG) and stays 0 when the original
// bytes were returned unchanged. MetadataPreserved reports that metadata
// stripping was requested but the original bytes were kept because the stripped
// candidate would have exceeded the target or produced a larger file.
type CompressOutcome struct {
	SizeBytes          int64
	Width              int
	Height             int
	Quality            int
	DownscaleRounds    int
	TargetMet          bool
	AlreadyUnderTarget bool
	MetadataPreserved  bool
}

// compressAttempt is one encoded candidate considered by the target search.
type compressAttempt struct {
	bytes     []byte
	quality   int
	size      int64
	targetMet bool
}

// CompressToTarget is CompressToTargetContext with a background context.
func CompressToTarget(input []byte, opts CompressOptions) ([]byte, CompressOutcome, error) {
	return CompressToTargetContext(context.Background(), input, opts)
}

// CompressToTargetContext encodes input so its size does not exceed
// opts.TargetSizeBytes when that goal is reachable. JPEG and WebP bisect the
// encoder quality, PNG walks a lossless-to-palette ladder, and when the target
// is still unreachable the longest edge shrinks up to three times.
//
// The best candidate found is always returned, with TargetMet reporting whether
// the goal was actually reached. When the input already satisfies the target
// and needs no format or size change, the original bytes are preferred: they are
// returned untouched unless metadata stripping is requested and a stripped
// candidate is both under the target and smaller (MetadataPreserved reports the
// fallback). The context is checked before every encode and between downscale
// rounds; cancellation returns an error wrapping ctx.Err().
func CompressToTargetContext(ctx context.Context, input []byte, opts CompressOptions) ([]byte, CompressOutcome, error) {
	outcome := CompressOutcome{}

	if err := compressCheckContext(ctx); err != nil {
		return nil, outcome, err
	}

	if opts.TargetSizeBytes <= 0 {
		return nil, outcome, fmt.Errorf("target size must be greater than zero")
	}

	format, imageType, formatErr := compressImageType(opts.Format)
	if formatErr != nil {
		return nil, outcome, formatErr
	}

	metadata, metaErr := bimg.NewImage(input).Metadata()
	if metaErr != nil {
		return nil, outcome, fmt.Errorf("decode image: %w", metaErr)
	}
	if metadata.Size.Width < 1 || metadata.Size.Height < 1 {
		return nil, outcome, fmt.Errorf("decode image: invalid dimensions %dx%d", metadata.Size.Width, metadata.Size.Height)
	}

	// Metadata reports the stored axes; bimg follows the EXIF orientation when
	// processing, so the resize search must use the visual axes.
	storedWidth, storedHeight := metadata.Size.Width, metadata.Size.Height
	sourceWidth, sourceHeight := storedWidth, storedHeight
	if metadata.Orientation >= 5 && metadata.Orientation <= 8 {
		sourceWidth, sourceHeight = sourceHeight, sourceWidth
	}

	inputFormat := normalizeFormatName(bimg.DetermineImageTypeName(input))
	inputSize := int64(len(input))
	copyEligible := inputFormat == format && inputSize <= opts.TargetSizeBytes

	// Quality is a ceiling, never a floor: the search stops at or below it.
	ceiling := opts.Quality
	if ceiling <= 0 || ceiling > compressMaxQuality {
		ceiling = compressMaxQuality
	}

	width, height := sourceWidth, sourceHeight
	capped := false
	if opts.MaxDimension > 0 {
		width, height, capped = compressScaledDimensions(width, height, opts.MaxDimension)
	}

	// An untouched copy is only valid without a requested resize. Keeping the
	// original respects both the size target and the source metadata.
	if copyEligible && !capped {
		if !opts.StripMetadata {
			return input, CompressOutcome{
				SizeBytes:          inputSize,
				Width:              storedWidth,
				Height:             storedHeight,
				TargetMet:          true,
				AlreadyUnderTarget: true,
			}, nil
		}

		attempt, searchErr := compressSearch(ctx, input, imageType, width, height, ceiling, opts, false)
		if searchErr != nil {
			return nil, outcome, searchErr
		}

		if attempt.targetMet && attempt.size < inputSize {
			size, sizeErr := bimg.NewImage(attempt.bytes).Size()
			if sizeErr != nil {
				return nil, outcome, fmt.Errorf("read compressed image size: %w", sizeErr)
			}

			return attempt.bytes, CompressOutcome{
				SizeBytes: attempt.size,
				Width:     size.Width,
				Height:    size.Height,
				Quality:   attempt.quality,
				TargetMet: true,
			}, nil
		}

		return input, CompressOutcome{
			SizeBytes:          inputSize,
			Width:              storedWidth,
			Height:             storedHeight,
			TargetMet:          true,
			AlreadyUnderTarget: true,
			MetadataPreserved:  true,
		}, nil
	}

	rounds := 0
	resized := capped
	var best compressAttempt
	for {
		if err := compressCheckContext(ctx); err != nil {
			return nil, outcome, err
		}

		attempt, searchErr := compressSearch(ctx, input, imageType, width, height, ceiling, opts, resized)
		if searchErr != nil {
			return nil, outcome, searchErr
		}

		if best.bytes == nil || attempt.size < best.size {
			best = attempt
		}
		if attempt.targetMet {
			best = attempt
			break
		}

		if !opts.AllowDownscale || rounds >= compressMaxDownscale {
			break
		}

		nextWidth, nextHeight, canShrink := compressDownscaleDimensions(width, height, opts.TargetSizeBytes, attempt.size)
		if !canShrink {
			break
		}

		width, height = nextWidth, nextHeight
		rounds++
		resized = true
	}

	size, sizeErr := bimg.NewImage(best.bytes).Size()
	if sizeErr != nil {
		return nil, outcome, fmt.Errorf("read compressed image size: %w", sizeErr)
	}

	outcome = CompressOutcome{
		SizeBytes:       best.size,
		Width:           size.Width,
		Height:          size.Height,
		Quality:         best.quality,
		DownscaleRounds: rounds,
		TargetMet:       best.targetMet,
	}

	return best.bytes, outcome, nil
}

// compressCheckContext reports a cancellation error that wraps ctx.Err so
// callers can classify it with errors.Is.
func compressCheckContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("compress cancelled: %w", ctx.Err())
	default:
		return nil
	}
}

// compressImageType resolves the requested encoder format. Only the three
// formats the tool advertises are accepted; animated and metadata-heavy
// containers are never produced.
func compressImageType(format string) (string, bimg.ImageType, error) {
	normalized := normalizeFormatName(format)
	if normalized == "" {
		return "", bimg.UNKNOWN, fmt.Errorf("output format is required: use jpeg, png or webp")
	}

	switch normalized {
	case "jpeg", "png", "webp":
	default:
		return "", bimg.UNKNOWN, fmt.Errorf("unsupported image format: %s (use jpeg, png or webp)", normalized)
	}

	imageType := mapFormatToImageType(normalized)
	if !bimg.IsTypeSupportedSave(imageType) {
		return "", bimg.UNKNOWN, fmt.Errorf("output format %s is not supported by this libvips build", normalized)
	}

	return normalized, imageType, nil
}

// compressSearch runs the format-specific quality search at one pixel size.
// Dimensions are only applied when resize is true, so a same-size re-encode
// never triggers a scale-1 resample.
func compressSearch(ctx context.Context, input []byte, imageType bimg.ImageType, width, height, ceiling int, opts CompressOptions, resize bool) (compressAttempt, error) {
	if imageType == bimg.PNG {
		return compressSearchPNG(ctx, input, width, height, opts, resize)
	}

	return compressSearchQuality(ctx, input, imageType, width, height, ceiling, opts, resize)
}

// compressSearchQuality bisects the quality range and keeps the highest quality
// whose output fits the target. The step cap guarantees a bounded amount of
// encoding even for an unreachable target.
func compressSearchQuality(ctx context.Context, input []byte, imageType bimg.ImageType, width, height, ceiling int, opts CompressOptions, resize bool) (compressAttempt, error) {
	low := compressJPEGMinQuality
	if imageType == bimg.WEBP {
		low = compressWebPMinQuality
	}

	high := ceiling
	if high < low {
		// An explicit ceiling below the format minimum wins: honoring the
		// request matters more than the default floor.
		low = high
	}

	var best compressAttempt
	var smallest compressAttempt

	for step := 0; step < compressQualitySteps && low <= high; step++ {
		if err := compressCheckContext(ctx); err != nil {
			return compressAttempt{}, err
		}

		quality := (low + high) / 2
		encoded, err := compressEncode(input, compressEncodeOptions{
			imageType:     imageType,
			width:         width,
			height:        height,
			resize:        resize,
			quality:       quality,
			stripMetadata: opts.StripMetadata,
			background:    opts.Background,
		})
		if err != nil {
			return compressAttempt{}, fmt.Errorf("encode %s at quality %d: %w", bimg.ImageTypeName(imageType), quality, err)
		}

		attempt := compressAttempt{bytes: encoded, quality: quality, size: int64(len(encoded))}
		if attempt.size <= opts.TargetSizeBytes {
			attempt.targetMet = true
			best = attempt
			low = quality + 1
			continue
		}

		high = quality - 1
		if smallest.bytes == nil || attempt.size < smallest.size {
			smallest = attempt
		}
	}

	if best.bytes != nil {
		return best, nil
	}

	return smallest, nil
}

// compressSearchPNG walks the deterministic PNG ladder: lossless first, then
// palette quantisation at 80, 60 and 40. The first candidate that fits wins and
// the smallest over-target candidate is the honest best effort.
func compressSearchPNG(ctx context.Context, input []byte, width, height int, opts CompressOptions, resize bool) (compressAttempt, error) {
	steps := []struct {
		palette bool
		quality int
	}{
		{palette: false, quality: compressLosslessQuality},
		{palette: true, quality: 80},
		{palette: true, quality: 60},
		{palette: true, quality: 40},
	}

	var smallest compressAttempt
	for _, step := range steps {
		if err := compressCheckContext(ctx); err != nil {
			return compressAttempt{}, err
		}

		encoded, err := compressEncode(input, compressEncodeOptions{
			imageType:     bimg.PNG,
			width:         width,
			height:        height,
			resize:        resize,
			quality:       step.quality,
			palette:       step.palette,
			stripMetadata: opts.StripMetadata,
			background:    opts.Background,
		})
		if err != nil {
			return compressAttempt{}, fmt.Errorf("encode png (palette %t, quality %d): %w", step.palette, step.quality, err)
		}

		attempt := compressAttempt{bytes: encoded, quality: step.quality, size: int64(len(encoded))}
		if attempt.size <= opts.TargetSizeBytes {
			attempt.targetMet = true
			return attempt, nil
		}

		if smallest.bytes == nil || attempt.size < smallest.size {
			smallest = attempt
		}
	}

	return smallest, nil
}

type compressEncodeOptions struct {
	imageType     bimg.ImageType
	width         int
	height        int
	resize        bool
	quality       int
	palette       bool
	stripMetadata bool
	background    *bimg.Color
}

// compressEncode performs one bounded encode. Dimensions are only set when a
// real resize is requested; Enlarge stays false so nothing is ever upscaled, and
// the background is only used for JPEG alpha flattening.
func compressEncode(input []byte, opts compressEncodeOptions) ([]byte, error) {
	bimgOptions := bimg.Options{
		Type:          opts.imageType,
		Quality:       opts.quality,
		StripMetadata: opts.stripMetadata,
		Enlarge:       false,
	}

	if opts.resize && opts.width > 0 && opts.height > 0 {
		bimgOptions.Width = opts.width
		bimgOptions.Height = opts.height
	}

	switch opts.imageType {
	case bimg.JPEG:
		bimgOptions.Interlace = true
		if opts.background != nil {
			bimgOptions.Background = *opts.background
		}
	case bimg.PNG:
		bimgOptions.Compression = 9
		if opts.palette {
			bimgOptions.Palette = true
			// bimg maps Speed to the libvips effort (10 - speed).
			bimgOptions.Speed = 3
		}
	}

	return bimg.NewImage(input).Process(bimgOptions)
}

// compressDownscaleDimensions computes the next longest edge from the ratio of
// the target to the best encoded size, with a small safety margin. It never
// upscales and never goes below the floor.
func compressDownscaleDimensions(width, height int, targetSize, bestSize int64) (int, int, bool) {
	longest := maxInt(width, height)
	if bestSize <= 0 || targetSize <= 0 || longest <= compressDownscaleFloor {
		return 0, 0, false
	}

	ratio := math.Sqrt(float64(targetSize)/float64(bestSize)) * compressDownscaleSafety
	newLongest := int(math.Round(float64(longest) * ratio))
	if newLongest < compressDownscaleFloor {
		newLongest = compressDownscaleFloor
	}
	if newLongest >= longest {
		return 0, 0, false
	}

	newWidth, newHeight, _ := compressScaledDimensions(width, height, newLongest)
	return newWidth, newHeight, true
}

// compressScaledDimensions caps the longest edge at limit while preserving the
// aspect ratio. It reports false when the image already fits.
func compressScaledDimensions(width, height, limit int) (int, int, bool) {
	longest := maxInt(width, height)
	if limit <= 0 || longest <= limit {
		return width, height, false
	}

	scale := float64(limit) / float64(longest)
	if width >= height {
		return limit, maxInt(1, int(math.Round(float64(height)*scale))), true
	}

	return maxInt(1, int(math.Round(float64(width)*scale))), limit, true
}
