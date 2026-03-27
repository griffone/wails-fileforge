package models

type JobErrorV1 struct {
	Code       string         `json:"code"`
	DetailCode string         `json:"detail_code,omitempty"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
}

type JobRequestV1 struct {
	ToolID     string         `json:"toolId"`
	Mode       string         `json:"mode"` // single | batch
	InputPaths []string       `json:"inputPaths"`
	OutputDir  string         `json:"outputDir"`
	Options    map[string]any `json:"options"`
	Workers    int            `json:"workers,omitempty"`
}

type JobProgressV1 struct {
	Current int    `json:"current"`
	Total   int    `json:"total"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type JobResultItemV1 struct {
	InputPath   string      `json:"inputPath"`
	OutputPath  string      `json:"outputPath"`
	Outputs     []string    `json:"outputs,omitempty"`
	OutputCount int         `json:"outputCount,omitempty"`
	Success     bool        `json:"success"`
	Message     string      `json:"message"`
	Error       *JobErrorV1 `json:"error,omitempty"`
}

type JobResultV1 struct {
	JobID     string            `json:"jobId"`
	Success   bool              `json:"success"`
	Message   string            `json:"message"`
	ToolID    string            `json:"toolId"`
	Status    string            `json:"status"`
	Progress  JobProgressV1     `json:"progress"`
	Items     []JobResultItemV1 `json:"items"`
	Error     *JobErrorV1       `json:"error,omitempty"`
	StartedAt int64             `json:"startedAt"`
	EndedAt   int64             `json:"endedAt,omitempty"`
}

type ValidateJobResponseV1 struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Valid   bool        `json:"valid"`
	Error   *JobErrorV1 `json:"error,omitempty"`
}

type RunJobResponseV1 struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	JobID   string      `json:"jobId"`
	Status  string      `json:"status"`
	Error   *JobErrorV1 `json:"error,omitempty"`
}

type JobStatusResponseV1 struct {
	Success bool         `json:"success"`
	Message string       `json:"message"`
	Found   bool         `json:"found"`
	Result  *JobResultV1 `json:"result,omitempty"`
	Error   *JobErrorV1  `json:"error,omitempty"`
}

type CancelJobResponseV1 struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	JobID   string      `json:"jobId"`
	Error   *JobErrorV1 `json:"error,omitempty"`
}

type PDFPreviewSourceResponseV1 struct {
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
	DataBase64 string      `json:"dataBase64,omitempty"`
	MimeType   string      `json:"mimeType,omitempty"`
	Error      *JobErrorV1 `json:"error,omitempty"`
}

type ImagePreviewSourceResponseV1 struct {
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
	DataBase64 string      `json:"dataBase64,omitempty"`
	MimeType   string      `json:"mimeType,omitempty"`
	Width      int         `json:"width,omitempty"`
	Height     int         `json:"height,omitempty"`
	Error      *JobErrorV1 `json:"error,omitempty"`
}

type ImageCropPreviewRequestV1 struct {
	InputPath   string `json:"inputPath"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	RatioPreset string `json:"ratioPreset,omitempty"`
	Format      string `json:"format,omitempty"`
}

type ImageCropPreviewResponseV1 struct {
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
	DataBase64 string      `json:"dataBase64,omitempty"`
	MimeType   string      `json:"mimeType,omitempty"`
	Width      int         `json:"width,omitempty"`
	Height     int         `json:"height,omitempty"`
	Error      *JobErrorV1 `json:"error,omitempty"`
}
