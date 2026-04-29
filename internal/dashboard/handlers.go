package dashboard

import (
	"net/http"
	"sort"
	"time"

	"agent-gate/internal/store"
)

type sessionRow struct {
	SessionID  string
	Host       string
	StartedAt  time.Time
	EventCount int
	HasFlags   bool
}

func handleSessionsList(opts Options, r *renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		rows, err := opts.Store.Index().Query(store.QueryFilter{Limit: 1000})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		groups := map[string]*sessionRow{}
		for _, ix := range rows {
			sid := ix.SessionID
			if sid == "" {
				sid = "(no session)"
			}
			g, ok := groups[sid]
			if !ok {
				g = &sessionRow{SessionID: sid, Host: ix.Host, StartedAt: ix.StartedAt}
				groups[sid] = g
			}
			g.EventCount++
			if ix.StartedAt.After(g.StartedAt) {
				g.StartedAt = ix.StartedAt
			}
			if ix.FlagCodes != "" {
				g.HasFlags = true
			}
		}
		out := make([]*sessionRow, 0, len(groups))
		for _, g := range groups {
			out = append(out, g)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })

		r.Render(w, req, "sessions", map[string]any{"Sessions": out})
	}
}
