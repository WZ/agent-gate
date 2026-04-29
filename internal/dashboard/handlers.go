package dashboard

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-gate/internal/dismissals"
	"agent-gate/internal/redactor"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
)

type sessionRow struct {
	Label      string
	Key        string
	Host       string
	StartedAt  time.Time
	EventCount int
	HasFlags   bool
}

// normalizeHost strips any port suffix from a stored host value so that
// legacy rows with ":443" collapse into the same bucket as new rows.
func normalizeHost(h string) string {
	if idx := strings.LastIndex(h, ":"); idx >= 0 {
		return h[:idx]
	}
	return h
}

func handleSessionsList(opts Options, r *renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		filter := store.QueryFilter{Limit: 1000}
		q := req.URL.Query()
		filter.Host = q.Get("host")
		if v := q.Get("since"); v != "" {
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				filter.Since = ts
			}
		}
		if v := q.Get("until"); v != "" {
			if ts, err := time.Parse(time.RFC3339, v); err == nil {
				filter.Until = ts
			}
		}

		rows, err := opts.Store.Index().Query(filter)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		groups := map[string]*sessionRow{}
		for _, ix := range rows {
			normHost := normalizeHost(ix.Host)
			var key, label string
			if ix.SessionID != "" {
				key = ix.SessionID
				label = ix.SessionID
			} else {
				key = "host:" + normHost
				label = "(host) " + normHost
			}
			g, ok := groups[key]
			if !ok {
				g = &sessionRow{Label: label, Key: key, Host: normHost, StartedAt: ix.StartedAt}
				groups[key] = g
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

		r.Render(w, req, "sessions", map[string]any{
			"Sessions": out,
			"Filter":   filter,
		})
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

		var (
			label   string
			allRows []store.IndexRow
			err     error
		)

		if strings.HasPrefix(sid, "host:") {
			// Host-bucket: query by host (with and without port) and filter to empty session_id.
			h := strings.TrimPrefix(sid, "host:")
			label = "(host) " + h

			// Fetch rows matching the bare hostname.
			allRows, err = opts.Store.Index().Query(store.QueryFilter{Host: h, Limit: 1000})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			// Also fetch rows stored with the port-suffixed form (legacy data).
			// We try both :80 and :443 as well as a LIKE query via in-memory filter.
			// Simpler: fetch all rows, then in-memory filter by normalizeHost == h and SessionID == "".
			allWithPort, err2 := opts.Store.Index().Query(store.QueryFilter{Host: h + ":443", Limit: 1000})
			if err2 == nil {
				allRows = append(allRows, allWithPort...)
			}
			allWithPort80, err3 := opts.Store.Index().Query(store.QueryFilter{Host: h + ":80", Limit: 1000})
			if err3 == nil {
				allRows = append(allRows, allWithPort80...)
			}
		} else {
			label = sid
			allRows, err = opts.Store.Index().Query(store.QueryFilter{SessionID: sid, Limit: 1000})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}

		// Build events list (oldest-first).
		// For host-bucket, only include rows with empty SessionID.
		isHostBucket := strings.HasPrefix(sid, "host:")
		// Sort allRows by StartedAt ascending (they come back DESC from query).
		sort.Slice(allRows, func(i, j int) bool {
			return allRows[i].StartedAt.Before(allRows[j].StartedAt)
		})
		events := make([]eventRow, 0, len(allRows))
		for _, ix := range allRows {
			if isHostBucket && ix.SessionID != "" {
				continue
			}
			var codes []string
			if ix.FlagCodes != "" {
				codes = strings.Split(ix.FlagCodes, ",")
			}
			events = append(events, eventRow{
				ID: ix.ID, StartedAt: ix.StartedAt, Method: ix.Method,
				Host: normalizeHost(ix.Host), Path: ix.Path, Status: ix.Status, FlagCodes: codes,
			})
		}
		r.Render(w, req, "session_detail", map[string]any{"SessionID": sid, "Label": label, "Events": events})
	}
}

type eventDetail struct {
	ID          string
	SessionID   string
	Method      string
	URL         string
	Status      int
	StartedAt   time.Time
	ReqHeaders  http.Header
	RespHeaders http.Header
	ReqBody     string
	RespBody    string
	Flags       []types.Flag
	Raw         bool
	CaptureMode string
}

func handleEventDetail(opts Options, r *renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id := strings.TrimPrefix(req.URL.Path, "/events/")
		if id == "" {
			http.NotFound(w, req)
			return
		}
		raw := req.URL.Query().Get("raw") == "1"

		ix, err := opts.Store.Index().QueryByID(id)
		if err != nil {
			http.NotFound(w, req)
			return
		}
		bodyR, err := opts.Store.Body(id)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer bodyR.Close()
		rawBytes, err := io.ReadAll(bodyR)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var stored types.StoredEvent
		if err := json.Unmarshal(rawBytes, &stored); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		var reqBodyStr, respBodyStr string
		if raw {
			reqBodyStr = string(stored.ReqBody)
			respBodyStr = string(stored.RespBody)
			_ = opts.Dismissals.Add(dismissals.ScopeEvent, id, "raw_peek", ix.Host,
				"reviewer requested raw view of event "+id)
		} else {
			reqBodyStr = redactor.Redact(string(stored.ReqBody))
			respBodyStr = redactor.Redact(string(stored.RespBody))
		}

		detail := eventDetail{
			ID: id, SessionID: ix.SessionID, Method: ix.Method, URL: stored.URL,
			Status: ix.Status, StartedAt: ix.StartedAt,
			ReqHeaders:  redactor.RedactHeaders(stored.ReqHeaders),
			RespHeaders: redactor.RedactHeaders(stored.RespHeaders),
			ReqBody:     reqBodyStr, RespBody: respBodyStr,
			Flags: stored.Flags, Raw: raw,
			CaptureMode: ix.CaptureMode,
		}
		r.Render(w, req, "event_detail", detail)
	}
}

func handleClear(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if err := opts.Store.Clear(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// Plain-form POST: redirect back to /. (HTMX-aware clients can also follow this.)
		http.Redirect(w, req, "/", http.StatusSeeOther)
	}
}

func handleDismiss(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		scope := dismissals.Scope(req.FormValue("scope"))
		err := opts.Dismissals.Add(scope,
			req.FormValue("event_id"),
			req.FormValue("code"),
			req.FormValue("host"),
			req.FormValue("reason"),
		)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<span class="flag dismissed">dismissed</span>`))
	}
}

func handleTrust(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		host := req.FormValue("host")
		if err := opts.Allowlist.Add(host); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<span class="ok">trusted ` + html.EscapeString(host) + `</span>`))
	}
}
