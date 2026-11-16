package models

type PlannedOutputV1 struct {
	InputPath   string   `json:"inputPath"`
	OutputPath  string   `json:"outputPath"`
	OutputKind  string   `json:"outputKind"` // file | directory
	OutputCount int      `json:"outputCount,omitempty"`
	Exists      bool     `json:"exists"`
	Adjusted    bool     `json:"adjusted"`
	Warnings    []string `json:"warnings,omitempty"`
}

type PlanJobResponseV1 struct {
	Success          bool              `json:"success"`
	Message          string            `json:"message"`
	Outputs          []PlannedOutputV1 `json:"outputs"`
	EffectiveOptions map[string]any    `json:"effectiveOptions,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
	Error            *JobErrorV1       `json:"error,omitempty"`
}

type FileInfoV1 struct {
	Path             string  `json:"path"`
	Name             string  `json:"name"`
	Directory        string  `json:"directory"`
	Extension        string  `json:"extension"`
	SizeBytes        int64   `json:"sizeBytes"`
	ModifiedAt       int64   `json:"modifiedAt,omitempty"`
	MimeType         string  `json:"mimeType,omitempty"`
	Kind             string  `json:"kind"` // image|pdf|video|document|archive|other
	Width            int     `json:"width,omitempty"`
	Height           int     `json:"height,omitempty"`
	PageCount        int     `json:"pageCount,omitempty"`
	PageWidthPoints  float64 `json:"pageWidthPoints,omitempty"`
	PageHeightPoints float64 `json:"pageHeightPoints,omitempty"`
	DurationSeconds  float64 `json:"durationSeconds,omitempty"`
	Exists           bool    `json:"exists"`
	Error            string  `json:"error,omitempty"`
}

type InspectFilesResponseV1 struct {
	Success bool         `json:"success"`
	Message string       `json:"message"`
	Files   []FileInfoV1 `json:"files"`
}

type PathActionResponseV1 struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type MediaSourceResponseV1 struct {
	Success     bool   `json:"success"`
	URL         string `json:"url,omitempty"`
	FallbackURL string `json:"fallbackUrl,omitempty"`
	Message     string `json:"message"`
}
