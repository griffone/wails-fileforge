package models

// AppInfoResponse describes the running build for the About panel. It is
// filled from the ldflags injected at build time; a plain development build
// reports "dev", "none" and "unknown".
type AppInfoResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"builtAt"`
	// Release is true only for a stamped SemVer build that an updater may
	// compare. Development and dirty builds always report false.
	Release bool `json:"release"`
}
