package buildinfo

import "runtime"

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
	Go      = runtime.Version()
	OS      = runtime.GOOS
	Arch    = runtime.GOARCH
)
