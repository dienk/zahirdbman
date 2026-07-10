// Package zahirdbman embeds the web assets (templates and static files) so the
// compiled server binary is fully self-contained.
package zahirdbman

import "embed"

// WebFS holds the HTML templates and static assets (CSS, images, fonts) under
// the web/ directory. Naming web/static embeds its whole subtree, including the
// self-hosted fonts in web/static/fonts.
//
//go:embed web/templates/*.html web/static
var WebFS embed.FS
