package webassets

import "embed"

//go:embed *.html *.css *.js *.png *.svg *.ico *.webmanifest images/integrations/*.svg pierre/*.js.gz pierre/languages.json
var FS embed.FS
