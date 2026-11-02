//go:build headless

package web

import "io/fs"

func Assets() fs.FS { return nil }
