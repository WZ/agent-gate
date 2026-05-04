package dashboard

import (
	"net/http"
	"strings"
	"time"

	"agent-gate/internal/store"
)

// exploreRow is one row in the /explore results table.
type exploreRow struct {
	ID        string
	StartedAt time.Time
	Method    string
	Host      string
	Path      string
	Status    int
	PIICounts []PIICount // pre-aggregated across both sides
	FlagCodes []string
	Snippet   string // populated when ?q= is set; HTML-safe (post-escape, with <mark> wrapping)
}

// exploreView is the full template payload for /explore.
type exploreView struct {
	Rows        []exploreRow
	Q           string
	ActiveKinds []string
	ActiveHosts []string
	Preset      string
	Page        int
	HasNextPage bool
	TotalCount  int
	LatestEvent string
	HostOptions []hostOption
}

type hostOption struct {
	Name  string
	Count int
}

func handleExplore(opts Options, r *renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Path must be exactly "/explore" (or "/explore/" trailing slash).
		if path := strings.TrimSuffix(req.URL.Path, "/"); path != "/explore" {
			http.NotFound(w, req)
			return
		}
		// Phase 1: render every event, no filters. Filters land in Tasks 9-12.
		rows, err := opts.Store.Index().Query(store.QueryFilter{Limit: 500})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		view := exploreView{Rows: make([]exploreRow, 0, len(rows))}
		for _, ix := range rows {
			view.Rows = append(view.Rows, exploreRow{
				ID:        ix.ID,
				StartedAt: ix.StartedAt,
				Method:    ix.Method,
				Host:      normalizeHost(ix.Host),
				Path:      ix.Path,
				Status:    ix.Status,
				FlagCodes: splitFlagCodes(ix.FlagCodes),
			})
		}
		view.TotalCount = len(view.Rows)
		if view.TotalCount > 0 {
			view.LatestEvent = view.Rows[0].StartedAt.Format("2006-01-02 15:04:05")
		}
		r.Render(w, req, "explore", view)
	}
}
