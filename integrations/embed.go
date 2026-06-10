package integrationassets

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed all:*
var embedded embed.FS

type prefixFS struct {
	prefix string
	inner  fs.FS
}

func (p prefixFS) Open(name string) (fs.File, error) {
	rest, ok := strings.CutPrefix(name, p.prefix)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return p.inner.Open(rest)
}

func (p prefixFS) ReadFile(name string) ([]byte, error) {
	rest, ok := strings.CutPrefix(name, p.prefix)
	if !ok {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadFile(p.inner, rest)
}

// FS serves embedded integration files at paths prefixed with "integrations/",
// matching integrationMap source paths and integrationHashes keys.
var FS = prefixFS{prefix: "integrations/", inner: embedded}
