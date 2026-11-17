package models

// LocalCapabilityV1 is one external tool check shown in the Local capabilities
// panel. Required tools are needed for a whole tool family; optional ones only
// improve an existing workflow.
type LocalCapabilityV1 struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Available  bool   `json:"available"`
	Required   bool   `json:"required"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion,omitempty"`
}

// LocalCapabilitiesResponseV1 reports the local runtime surface: every checked
// binary with its guidance plus the installed OCR languages.
type LocalCapabilitiesResponseV1 struct {
	Success                bool                `json:"success"`
	Message                string              `json:"message"`
	Checks                 []LocalCapabilityV1 `json:"checks"`
	OCRLanguages           []string            `json:"ocrLanguages"`
	RecommendedOCRLanguage string              `json:"recommendedOcrLanguage,omitempty"`
}
