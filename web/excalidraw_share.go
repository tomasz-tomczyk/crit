package webassets

import (
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"path"
	"strings"
)

// Excalidraw canvas share assets, served next to the entry HTML so relative
// <script src> and FontFace URLs resolve under crit-web's /r/:token/raw/…
// tree (the preview host does not expose the app's /rough.min.js).
var excalidrawShareScripts = []string{
	"rough.min.js",
	"freehand.min.js",
	"crit-renderer-excalidraw.js",
	"crit-excalidraw-canvas.js",
}

// BuildExcalidrawCanvasPage returns the canvas HTML for a scene: the embedded
// template with title and scene filled in, and absolute /asset script tags
// rewritten to relative names so a shared preview's raw tree can host them.
func BuildExcalidrawCanvasPage(title string, scene []byte) ([]byte, error) {
	page, err := FS.ReadFile("excalidraw-canvas.html")
	if err != nil {
		return nil, fmt.Errorf("excalidraw-canvas.html: %w", err)
	}
	sceneJSON, err := json.Marshal(string(scene))
	if err != nil {
		return nil, fmt.Errorf("marshal scene: %w", err)
	}
	body := strings.NewReplacer(
		"{{TITLE}}", html.EscapeString(title),
		"{{SCENE}}", string(sceneJSON),
		`src="/rough.min.js"`, `src="rough.min.js"`,
		`src="/freehand.min.js"`, `src="freehand.min.js"`,
		`src="/crit-renderer-excalidraw.js"`, `src="crit-renderer-excalidraw.js"`,
		`src="/crit-excalidraw-canvas.js"`, `src="crit-excalidraw-canvas.js"`,
	).Replace(string(page))
	return []byte(body), nil
}

// ExcalidrawShareAssets returns the JS and font files that must sit beside a
// shared canvas page. Keys are paths relative to the entry HTML's directory
// (e.g. "rough.min.js", "fonts/excalidraw/Virgil/Virgil-Regular.woff2").
func ExcalidrawShareAssets() (map[string][]byte, error) {
	out := make(map[string][]byte, len(excalidrawShareScripts)+24)
	for _, name := range excalidrawShareScripts {
		data, err := FS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = data
	}
	err := fs.WalkDir(FS, "fonts/excalidraw", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := FS.ReadFile(p)
		if err != nil {
			return err
		}
		out[path.Clean(p)] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
