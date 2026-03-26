//go:build production

package main

import (
	"embed"
	"io/fs"
)

//go:embed all:frontend/dist/frontend/browser
var embeddedAssets embed.FS

var assets fs.FS = embeddedAssets
