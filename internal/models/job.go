package models

type JobError struct {
	Code       string         `json:"code"`
	DetailCode string         `json:"detail_code,omitempty"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details,omitempty"`
}

type JobRequest struct {
	ToolID     string         `json:"toolId"`
	Mode       string         `json:"mode"` // single | batch
	InputPaths []string       `json:"inputPaths"`
	OutputDir  string         `json:"outputDir"`
	Options    map[string]any `json:"options"`
	Workers    int            `json:"workers,omitempty"`
}

type JobProgress struct {
	Current    int    `json:"current"`
	Total      int    `json:"total"`
	Stage      string `json:"stage"`
	Message    string `json:"message"`
	ETASeconds int    `json:"etaSeconds,omitempty"`
}

type JobProgressEvent struct {
	JobID    string      `json:"jobId"`
	Kind     string      `json:"kind,omitempty"`
	ToolID   string      `json:"toolId"`
	Status   string      `json:"status"`
	Progress JobProgress `json:"progress"`
}

type JobResultItem struct {
	InputPath   string    `json:"inputPath"`
	OutputPath  string    `json:"outputPath"`
	Outputs     []string  `json:"outputs,omitempty"`
	OutputCount int       `json:"outputCount,omitempty"`
	Attempts    int       `json:"attempts,omitempty"`
	RetryCount  int       `json:"retryCount,omitempty"`
	Success     bool      `json:"success"`
	Message     string    `json:"message"`
	Error       *JobError `json:"error,omitempty"`
}

type JobResult struct {
	JobID     string          `json:"jobId"`
	Kind      string          `json:"kind,omitempty"`
	Success   bool            `json:"success"`
	Message   string          `json:"message"`
	ToolID    string          `json:"toolId"`
	Status    string          `json:"status"`
	Progress  JobProgress     `json:"progress"`
	Items     []JobResultItem `json:"items"`
	Error     *JobError       `json:"error,omitempty"`
	StartedAt int64           `json:"startedAt"`
	EndedAt   int64           `json:"endedAt,omitempty"`
}

type ValidateJobResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	Valid   bool      `json:"valid"`
	Error   *JobError `json:"error,omitempty"`
}

type RunJobResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	JobID   string    `json:"jobId"`
	Status  string    `json:"status"`
	Error   *JobError `json:"error,omitempty"`
}

type JobStatusResponse struct {
	Success bool       `json:"success"`
	Message string     `json:"message"`
	Found   bool       `json:"found"`
	Result  *JobResult `json:"result,omitempty"`
	Error   *JobError  `json:"error,omitempty"`
}

type CancelJobResponse struct {
	Success bool      `json:"success"`
	Message string    `json:"message"`
	JobID   string    `json:"jobId"`
	Error   *JobError `json:"error,omitempty"`
}

// JobListResponse lists the active and recent engine jobs, newest first. It
// backs the background task activity view so the UI can render every tracked
// task without polling each job id.
type JobListResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Jobs    []JobResult `json:"jobs"`
}

type PDFPreviewSourceResponse struct {
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	DataBase64 string    `json:"dataBase64,omitempty"`
	MimeType   string    `json:"mimeType,omitempty"`
	Error      *JobError `json:"error,omitempty"`
}

type ImagePreviewSourceResponse struct {
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	DataBase64 string    `json:"dataBase64,omitempty"`
	MimeType   string    `json:"mimeType,omitempty"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	Error      *JobError `json:"error,omitempty"`
}

type ImageCropPreviewRequest struct {
	InputPath   string `json:"inputPath"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	RatioPreset string `json:"ratioPreset,omitempty"`
	Format      string `json:"format,omitempty"`
}

type ImageCropPreviewResponse struct {
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	DataBase64 string    `json:"dataBase64,omitempty"`
	MimeType   string    `json:"mimeType,omitempty"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	Error      *JobError `json:"error,omitempty"`
}

// ImageCropSuggestionResponse carries the suggested crop rectangle detected
// from a uniform image border. Coordinates are original-image pixels with the
// origin at the top-left. Found=false is a normal answer, not an error: it
// means no border was trustworthy enough to suggest.
type ImageCropSuggestionResponse struct {
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	Found      bool      `json:"found"`
	X          int       `json:"x"`
	Y          int       `json:"y"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Reason     string    `json:"reason,omitempty"`
	Confidence string    `json:"confidence,omitempty"`
	Error      *JobError `json:"error,omitempty"`
}

type ImageAnnotateOperation struct {
	Type          string  `json:"type"`
	X             int     `json:"x,omitempty"`
	Y             int     `json:"y,omitempty"`
	Width         int     `json:"width,omitempty"`
	Height        int     `json:"height,omitempty"`
	X2            int     `json:"x2,omitempty"`
	Y2            int     `json:"y2,omitempty"`
	Text          string  `json:"text,omitempty"`
	Color         string  `json:"color,omitempty"`
	Opacity       float64 `json:"opacity,omitempty"`
	StrokeWidth   int     `json:"strokeWidth,omitempty"`
	FontSize      int     `json:"fontSize,omitempty"`
	BlurIntensity int     `json:"blurIntensity,omitempty"`
}

type ImageAnnotatePreviewRequest struct {
	InputPath   string                   `json:"inputPath"`
	Operations  []ImageAnnotateOperation `json:"operations"`
	Format      string                   `json:"format,omitempty"`
	OutputColor string                   `json:"outputColor,omitempty"`
}

type ImageAnnotatePreviewResponse struct {
	Success    bool      `json:"success"`
	Message    string    `json:"message"`
	DataBase64 string    `json:"dataBase64,omitempty"`
	MimeType   string    `json:"mimeType,omitempty"`
	Width      int       `json:"width,omitempty"`
	Height     int       `json:"height,omitempty"`
	Error      *JobError `json:"error,omitempty"`
}
