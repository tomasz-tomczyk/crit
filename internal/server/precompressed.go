package server

import (
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// precompressedPrefix is the URL (and embedded directory) holding the
// @pierre/diffs bundle. It is stored as *.js.gz because Go embeds files
// uncompressed and the Shiki grammar chunks would otherwise add ~6MB to the
// binary (scripts/build-pierre.mjs).
const precompressedPrefix = "/pierre/"

// servePrecompressed serves /pierre/<name>.js from the embedded
// pierre/<name>.js.gz. Browsers get the gzip bytes as-is with
// Content-Encoding: gzip; a client that does not accept gzip gets it
// decompressed on the fly.
func servePrecompressed(assets fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if path.Ext(name) != ".js" || path.Dir(name) != strings.Trim(precompressedPrefix, "/") || !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		f, err := assets.Open(name + ".gz")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		h := w.Header()
		h.Set("Content-Type", "text/javascript; charset=utf-8")
		h.Set("Vary", "Accept-Encoding")
		// Chunk names are content hashes; the entry and worker are rebuilt
		// per release, so let the browser revalidate rather than cache hard.
		h.Set("Cache-Control", "no-cache")
		if acceptsGzip(r) {
			h.Set("Content-Encoding", "gzip")
			if r.Method == http.MethodHead {
				return
			}
			_, _ = io.Copy(w, f)
			return
		}
		zr, err := gzip.NewReader(f)
		if err != nil {
			http.Error(w, "corrupt embedded asset", http.StatusInternalServerError)
			return
		}
		defer zr.Close()
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.Copy(w, zr)
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		enc, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(enc), "gzip") {
			continue
		}
		// "gzip;q=0" explicitly refuses the encoding.
		if qv, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if q, err := strconv.ParseFloat(qv, 64); err == nil && q == 0 {
				return false
			}
		}
		return true
	}
	return false
}
