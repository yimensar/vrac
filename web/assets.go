//go:build !headless

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var bundle embed.FS

func Assets() fs.FS { result, _ := fs.Sub(bundle, "dist"); return result }
