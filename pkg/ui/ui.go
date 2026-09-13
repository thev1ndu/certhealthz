// Package ui embeds the built React dashboard (see /dashboard) into
// the certhealthz binary so `certhealthz dashboard` can serve it with no
// separate static file deployment. Run `make dashboard-build` to populate
// dist/ with the real build before compiling a release binary; until then
// dist/ holds a placeholder page.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded dashboard build, rooted at dist/ so paths match
// what a static file server expects (index.html, assets/, ...).
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// Handler serves the embedded dashboard as static files, falling back to
// index.html for any unknown path so client-side routing still works.
func Handler() (http.Handler, error) {
	sub, err := FS()
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := trimLeadingSlash(r.URL.Path)
		if name == "" {
			name = "."
		}
		if info, err := fs.Stat(sub, name); err != nil || info.IsDir() {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}
