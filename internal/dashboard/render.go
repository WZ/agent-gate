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

// NotFound renders a styled 404 page in the dashboard chrome instead of
// the default plain-text "404 page not found". Title is the heading
// (e.g. "Event not found"); message is the body line.
//
// Pre-renders into a buffer so we can set the 404 status before any
// content is written; Render writes the headers itself once it starts
// executing the template, which is too late for a status override.
func (r *renderer) NotFound(w http.ResponseWriter, req *http.Request, title, message string) {
	data := map[string]any{"Title": title, "Message": message}
	var fragment bytes.Buffer
	if err := r.full.ExecuteTemplate(&fragment, "not_found", data); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if req.Header.Get("HX-Request") == "true" {
		_, _ = w.Write(fragment.Bytes())
		return
	}
	combined := struct {
		Content     template.HTML
		ActiveRoute string
	}{
		Content:     template.HTML(fragment.String()), //nolint:gosec // our own template
		ActiveRoute: activeRoute(req.URL.Path),
	}
	_ = r.full.ExecuteTemplate(w, "base", combined)
}
