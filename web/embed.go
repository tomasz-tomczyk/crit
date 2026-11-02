package webassets

import "embed"

//go:embed *.html *.css *.js *.png *.svg *.ico *.webmanifest images/integrations/*.svg pierre/*.js.gz fonts/excalidraw
var FS embed.FS
