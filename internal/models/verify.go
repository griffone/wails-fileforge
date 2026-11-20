package models

type VerificationCheck struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Passed bool   `json:"passed"`
	Value  string `json:"value,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type VerifiedOutput struct {
	Path      string              `json:"path"`
	Exists    bool                `json:"exists"`
	SizeBytes int64               `json:"sizeBytes"`
	Kind      string              `json:"kind,omitempty"`
	Checks    []VerificationCheck `json:"checks"`
	Warnings  []string            `json:"warnings,omitempty"`
}

type VerificationReport struct {
	Success  bool             `json:"success"`
	Message  string           `json:"message"`
	Outputs  []VerifiedOutput `json:"outputs"`
	Warnings []string         `json:"warnings,omitempty"`
}
