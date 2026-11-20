package models

// TranscriptionCapabilitiesResponse reports whether local Whisper
// transcription can run. Available requires both the whisper-cli binary and a
// non-empty model file. Paths are the resolved ones: a user override wins when
// it exists, otherwise the installed defaults are used.
type TranscriptionCapabilitiesResponse struct {
	Success         bool     `json:"success"`
	Message         string   `json:"message"`
	Available       bool     `json:"available"`
	WhisperPath     string   `json:"whisperPath"`
	WhisperPresent  bool     `json:"whisperPresent"`
	ModelPath       string   `json:"modelPath"`
	ModelPresent    bool     `json:"modelPresent"`
	ModelName       string   `json:"modelName,omitempty"`
	ModelSizeBytes  int64    `json:"modelSizeBytes,omitempty"`
	Languages       []string `json:"languages"`
	DefaultLanguage string   `json:"defaultLanguage,omitempty"`
}
