package dashboard

import (
	"fmt"
	"net/http"
	"time"

	"agent-gate/internal/store"
)

const defaultLivePoll = 500 * time.Millisecond

// handleLive streams new event IDs as they arrive in the store. Polls SQLite at
// opts.LivePollInterval (default 500ms). Each newly seen event id is emitted as
// `data: <id>\n\n`.
func handleLive(opts Options) http.HandlerFunc {
	interval := opts.LivePollInterval
	if interval == 0 {
		interval = defaultLivePoll
	}
	return func(w http.ResponseWriter, req *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		seen := make(map[string]struct{})
		// Seed with anything already there so we don't replay history on connect.
		if rows, err := opts.Store.Index().Query(store.QueryFilter{Limit: 1000}); err == nil {
			for _, r := range rows {
				seen[r.ID] = struct{}{}
			}
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-req.Context().Done():
				return
			case <-ticker.C:
				rows, err := opts.Store.Index().Query(store.QueryFilter{Limit: 200})
				if err != nil {
					continue
				}
				// rows are newest-first; emit oldest-first so consumers see chronological order.
				for i := len(rows) - 1; i >= 0; i-- {
					r := rows[i]
					if _, dup := seen[r.ID]; dup {
						continue
					}
					seen[r.ID] = struct{}{}
					fmt.Fprintf(w, "data: %s\n\n", r.ID)
				}
				fl.Flush()
			}
		}
	}
}
