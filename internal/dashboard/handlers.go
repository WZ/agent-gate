package dashboard

import "net/http"

// handleSessionsList is a stub here; Task 11 will wire it to the store.
func handleSessionsList(opts Options, r *renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.Render(w, req, "sessions", map[string]any{"Sessions": nil})
	}
}
