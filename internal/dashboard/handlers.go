package dashboard

import (
	"net/http"
	"sort"
	"strings"
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

type eventRow struct {
	ID        string
	StartedAt time.Time
	Method    string
	Host      string
	Path      string
	Status    int
	FlagCodes []string
}

func handleSessionDetail(opts Options, r *renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		sid := strings.TrimPrefix(req.URL.Path, "/sessions/")
		if sid == "" {
			http.NotFound(w, req)
			return
		}
		rows, err := opts.Store.Index().Query(store.QueryFilter{SessionID: sid, Limit: 1000})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// Reverse to oldest-first for readability.
		events := make([]eventRow, 0, len(rows))
		for i := len(rows) - 1; i >= 0; i-- {
			ix := rows[i]
			var codes []string
			if ix.FlagCodes != "" {
				codes = strings.Split(ix.FlagCodes, ",")
			}
			events = append(events, eventRow{
				ID: ix.ID, StartedAt: ix.StartedAt, Method: ix.Method,
				Host: ix.Host, Path: ix.Path, Status: ix.Status, FlagCodes: codes,
			})
		}
		r.Render(w, req, "session_detail", map[string]any{"SessionID": sid, "Events": events})
	}
}
