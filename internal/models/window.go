package models

// ToolWindowResponse reports what an open-window request did. Created is false
// when the route already had a window and it was focused instead of duplicated.
type ToolWindowResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Route      string `json:"route"`
	WindowName string `json:"windowName"`
	Created    bool   `json:"created"`
}
