// Package zahirdbman embeds the web assets (templates and static files) so the
// compiled server binary is fully self-contained.
package zahirdbman

import "embed"

// WebFS holds the HTML templates and static CSS under the web/ directory.
//
//go:embed web/templates/*.html web/static/*
var WebFS embed.FS
