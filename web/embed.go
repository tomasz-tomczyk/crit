package webassets

import "embed"

//go:embed *.html *.css *.js *.png *.svg *.ico *.webmanifest images/integrations/*.svg pierre/*.js.gz
var FS embed.FS
