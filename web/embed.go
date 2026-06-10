package webassets

import "embed"

//go:embed *.html *.css *.js *.png *.svg *.ico *.webmanifest
var FS embed.FS
