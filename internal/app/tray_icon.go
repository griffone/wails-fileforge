package app

import (
	"bytes"
	"image"
	"image/png"

	"golang.org/x/image/draw"
)

// trayIconSize is the raster size handed to the platform tray. macOS resizes to
// the status bar thickness and Windows asks for a small icon, so shipping a
// 1024px source would make every host scale it (and push a 4 MB pixmap over
// D-Bus on Linux).
const trayIconSize = 32

// resizeTrayIcon downscales the source icon to tray size. A malformed source
// keeps the original bytes so a bad asset never stops the tray from starting.
func resizeTrayIcon(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}

	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw
	}

	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return raw
	}
	if width <= trayIconSize && height <= trayIconSize {
		return raw
	}

	scale := float64(trayIconSize) / float64(width)
	if height > width {
		scale = float64(trayIconSize) / float64(height)
	}
	targetWidth := max(1, int(float64(width)*scale))
	targetHeight := max(1, int(float64(height)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return raw
	}

	return out.Bytes()
}
