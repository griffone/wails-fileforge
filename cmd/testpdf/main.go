package main

import (
	apppkg "fileforge-desktop/internal/app"
	"fmt"
	"os"
)

func main() {
	// ensure FEATURE_UIUX_OVERHAUL_V1 isn't required for this API
	_ = os.Setenv("FEATURE_UIUX_OVERHAUL_V1", "true")
	a := apppkg.New()
	// sample path to test - use first arg or /tmp/test.pdf
	path := "/tmp/test.pdf"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	resp := a.GetPDFPreviewSourceV1(path)
	fmt.Printf("Success: %v\nMessage: %s\nError: %+v\nDataLen: %d\n", resp.Success, resp.Message, resp.Error, len(resp.DataBase64))
}
