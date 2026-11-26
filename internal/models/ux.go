package models

type PlannedOutput struct {
	InputPath   string   `json:"inputPath"`
	OutputPath  string   `json:"outputPath"`
	OutputKind  string   `json:"outputKind"` // file | directory
	OutputCount int      `json:"outputCount,omitempty"`
	Exists      bool     `json:"exists"`
	Adjusted    bool     `json:"adjusted"`
	Warnings    []string `json:"warnings,omitempty"`
}

type PlanJobResponse struct {
	Success          bool            `json:"success"`
	Message          string          `json:"message"`
	Outputs          []PlannedOutput `json:"outputs"`
	EffectiveOptions map[string]any  `json:"effectiveOptions,omitempty"`
	Warnings         []string        `json:"warnings,omitempty"`
	Error            *JobError       `json:"error,omitempty"`
}

type FileInfo struct {
	Path             string  `json:"path"`
	Name             string  `json:"name"`
	Directory        string  `json:"directory"`
	Extension        string  `json:"extension"`
	SizeBytes        int64   `json:"sizeBytes"`
	ModifiedAt       int64   `json:"modifiedAt,omitempty"`
	MimeType         string  `json:"mimeType,omitempty"`
	Kind             string  `json:"kind"` // image|pdf|video|audio|document|archive|other
	Width            int     `json:"width,omitempty"`
	Height           int     `json:"height,omitempty"`
	PageCount        int     `json:"pageCount,omitempty"`
	PageWidthPoints  float64 `json:"pageWidthPoints,omitempty"`
	PageHeightPoints float64 `json:"pageHeightPoints,omitempty"`
	DurationSeconds  float64 `json:"durationSeconds,omitempty"`
	VideoCodec       string  `json:"videoCodec,omitempty"`
	AudioCodec       string  `json:"audioCodec,omitempty"`
	Exists           bool    `json:"exists"`
	Error            string  `json:"error,omitempty"`
}

type InspectFilesResponse struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Files   []FileInfo `json:"files"`
}

type PathActionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type MediaSourceResponse struct {
	Success     bool   `json:"success"`
	URL         string `json:"url,omitempty"`
	FallbackURL string `json:"fallbackUrl,omitempty"`
	Message     string `json:"message"`
}
