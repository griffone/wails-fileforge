package models

type VerificationCheckV1 struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Passed bool   `json:"passed"`
	Value  string `json:"value,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type VerifiedOutputV1 struct {
	Path      string                `json:"path"`
	Exists    bool                  `json:"exists"`
	SizeBytes int64                 `json:"sizeBytes"`
	Kind      string                `json:"kind,omitempty"`
	Checks    []VerificationCheckV1 `json:"checks"`
	Warnings  []string              `json:"warnings,omitempty"`
}

type VerificationReportV1 struct {
	Success  bool               `json:"success"`
	Message  string             `json:"message"`
	Outputs  []VerifiedOutputV1 `json:"outputs"`
	Warnings []string           `json:"warnings,omitempty"`
}
