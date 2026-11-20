package toolchain

import "embed"

// Tesseract's PDF support assets are embedded instead of copied from the conda
// prefix. The conda-forge win-64 tesseract package ships configs/pdf and
// configs/txt but omits pdf.ttf, so an install that relied on the environment
// prefix failed on Windows with "the Tesseract package is missing pdf.ttf".
// These files are tiny and platform-independent, and keeping them in the
// binary makes the managed tessdata folder complete on every OS.
//
// The files come from the Tesseract project's Apache-2.0 licensed PDF support
// data (pdf.ttf and the pdf/txt configs).
//
//go:embed assets/pdf.ttf assets/configs/pdf assets/configs/txt
var tessdataPDFAssetsFS embed.FS
