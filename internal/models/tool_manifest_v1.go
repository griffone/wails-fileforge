package models

type ToolManifestV1 struct {
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
}

type ToolRuntimeStateV1 struct {
	Status  string `json:"status"` // enabled | disabled | degraded
	Reason  string `json:"reason,omitempty"`
	Healthy bool   `json:"healthy"`
}

type ToolCatalogEntryV1 struct {
	Manifest ToolManifestV1     `json:"manifest"`
	State    ToolRuntimeStateV1 `json:"state"`
}

type ListToolsResponseV1 struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	Tools   []ToolCatalogEntryV1 `json:"tools"`
}
