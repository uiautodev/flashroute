package frontend

import "embed"

// BuildFS contains the static SvelteKit build output.
//
//go:embed all:build
var BuildFS embed.FS
