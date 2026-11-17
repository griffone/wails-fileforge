package models

// OCRCapabilitiesResponseV1 reports whether the local OCR pipeline can run.
// Available requires pdftoppm, tesseract and at least one installed language
// (osd alone does not count). Languages excludes osd and is sorted.
type OCRCapabilitiesResponseV1 struct {
	Success             bool     `json:"success"`
	Message             string   `json:"message"`
	Available           bool     `json:"available"`
	PDFToPPM            bool     `json:"pdfToPpm"`
	Tesseract           bool     `json:"tesseract"`
	PDFToText           bool     `json:"pdfToText"`
	Languages           []string `json:"languages"`
	RecommendedLanguage string   `json:"recommendedLanguage"`
}
