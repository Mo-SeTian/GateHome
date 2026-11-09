// Package frontend contains the production Aurora Next console.
// Run make frontend before building or testing the Go application.
package frontend

import "embed"

//go:embed all:dist
var Dist embed.FS
