package models

type VerificationCheck struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Passed   bool   `json:"passed"`
	Advisory bool   `json:"advisory,omitempty"`
	Value    string `json:"value,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type VerifiedOutput struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"sizeBytes"`
	// ActualBytes and TargetBytes are set only when the job requested a target
	// size, so machine consumers never have to parse the human check value.
	ActualBytes int64               `json:"actualBytes,omitempty"`
	TargetBytes int64               `json:"targetBytes,omitempty"`
	Kind        string              `json:"kind,omitempty"`
	Checks      []VerificationCheck `json:"checks"`
	Warnings    []string            `json:"warnings,omitempty"`
}

type VerificationReport struct {
	Success  bool             `json:"success"`
	Message  string           `json:"message"`
	Outputs  []VerifiedOutput `json:"outputs"`
	Warnings []string         `json:"warnings,omitempty"`
}
