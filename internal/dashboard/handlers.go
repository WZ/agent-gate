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
