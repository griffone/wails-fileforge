package models

type ToolManifest struct {
	ToolID           string   `json:"toolId"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Domain           string   `json:"domain"`
	Capability       string   `json:"capability"`
	Version          string   `json:"version"`
	SupportsSingle   bool     `json:"supportsSingle"`
	SupportsBatch    bool     `json:"supportsBatch"`
	InputExtensions  []string `json:"inputExtensions"`
	OutputExtensions []string `json:"outputExtensions"`
	RuntimeDeps      []string `json:"runtimeDependencies"`
	Tags             []string `json:"tags"`

	// ResourceClass is the scheduler resource profile the tool consumes:
	// cpu, external, or io. An empty value keeps the engine's default mapping.
	ResourceClass string `json:"resourceClass,omitempty"`
	// MaxWorkers caps concurrent per-item work inside one job. It is a ceiling,
	// not a request: the scheduler still bounds the job by its class capacity.
	// Zero keeps the engine default.
	MaxWorkers int `json:"maxWorkers,omitempty"`
}

type ToolRuntimeState struct {
	Status  string `json:"status"` // enabled | disabled | degraded
	Reason  string `json:"reason,omitempty"`
	Healthy bool   `json:"healthy"`
}

type ToolCatalogEntry struct {
	Manifest ToolManifest     `json:"manifest"`
	State    ToolRuntimeState `json:"state"`
}

type ListToolsResponse struct {
	Success bool               `json:"success"`
	Message string             `json:"message"`
	Tools   []ToolCatalogEntry `json:"tools"`
}
