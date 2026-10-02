package main

import "runtime"

// These can be overridden at build time via
//   go build -ldflags "-X main.buildOS=windows -X main.buildArch=amd64"
var (
	buildOS   = runtime.GOOS
	buildArch = runtime.GOARCH
)