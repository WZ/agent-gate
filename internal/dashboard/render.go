package dashboard

import (
	"bytes"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

type renderer struct {
	full *template.Template
}

func newRenderer() *renderer {
	tplFS, _ := fs.Sub(assets, "templates")
	funcs := template.FuncMap{
		"hasSensitivePII": HasSensitivePII,
		"piiKindOptions":  piiKindOptions,
		"hasKind":         hasKind,
		"filterURL":       filterURL,
		"presetOptions":   presetOptions,
		"join":            strings.Join,
		"add":             func(a, b int) int { return a + b },
		"sub":             func(a, b int) int { return a - b },
	}
	full := template.Must(template.New("base").Funcs(funcs).ParseFS(tplFS, "*.html"))
	return &renderer{full: full}
}

// Render writes a full page or a fragment depending on the HX-Request header.
// For full-page requests the named fragment is pre-rendered into a safe
// template.HTML value so that the base template does not need a dynamic
// template-name call (which html/template does not support).
func (r *renderer) Render(w http.ResponseWriter, req *http.Request, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if req.Header.Get("HX-Request") == "true" {
		if err := r.full.ExecuteTemplate(w, name, data); err != nil {
			http.Error(w, err.Error(), 500)
		}
		return
	}
	// Pre-render the content fragment.
	var buf bytes.Buffer
	if err := r.full.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	combined := struct {
		Content     template.HTML
		ActiveRoute string // "operations" or "explore"
	}{
		Content:     template.HTML(buf.String()), //nolint:gosec // fragment rendered by our own templates
		ActiveRoute: activeRoute(req.URL.Path),
	}
	if err := r.full.ExecuteTemplate(w, "base", combined); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

// activeRoute maps the request URL path onto a top-nav identifier.
// Default is "operations" (the sessions list at `/`); paths starting
// with `/explore` are "explore".
func activeRoute(path string) string {
	if strings.HasPrefix(path, "/explore") {
		return "explore"
	}
	return "operations"
}
