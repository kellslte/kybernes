// Package templates holds the embedded project layers: base, arch/*, router/*, feature/*, db/*, extras/*.
package templates

import "embed"

//go:embed all:base all:arch all:router all:feature all:db all:extras
var FS embed.FS
