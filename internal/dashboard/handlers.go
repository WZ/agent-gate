package dashboard

import (
	"encoding/json"
	"html"
	"html/template"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-gate/internal/dismissals"
	"agent-gate/internal/pii"
	"agent-gate/internal/redactor"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
)

type severitySummary struct {
	High    int
	Medium  int
	Low     int
	Info    int
	Unknown int
}

type dashboardSummary struct {
	EventCount        int
	GroupCount        int
	FlaggedGroupCount int
	LatestEvent       time.Time
	LatestEventLabel  string
	Severity          severitySummary
	TopFlags          []flagSummary
}

type flagSummary struct {
	Code     string
	Severity string
	Count    int
}

type sessionRow struct {
	Label      string
	Key        string
	Host       string
	StartedAt  time.Time
	EventCount int
	FlagCount  int
	HasFlags   bool
	Severity   string
}

// normalizeHost strips any port suffix from a stored host value so that
// legacy rows with ":443" collapse into the same bucket as new rows.
func normalizeHost(h string) string {
	if idx := strings.LastIndex(h, ":"); idx >= 0 {
		return h[:idx]
	}
	return h
}

func severityForFlagCode(code string) string {
	switch code {
	case "host_not_allowlisted", "secret_in_request", "env_in_tool_result":
		return "high"
	case "oversized_request", "unknown_mcp_endpoint":
		return "medium"
	case "oversized_response":
		return "low"
	case "permissive_capture", "parse_error":
		return "info"
	default:
		return "unknown"
	}
}

func splitFlagCodes(codes string) []string {
	if codes == "" {
		return nil
	}
	parts := strings.Split(codes, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		code := strings.TrimSpace(part)
		if code != "" {
			out = append(out, code)
		}
	}
	return out
}

func maxSeverity(a, b string) string {
	rank := map[string]int{
		"":        0,
		"info":    1,
		"low":     2,
		"unknown": 3,
		"medium":  4,
		"high":    5,
	}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func buildDashboardSummary(rows []store.IndexRow, groups []*sessionRow) dashboardSummary {
	summary := dashboardSummary{
		EventCount: len(rows),
		GroupCount: len(groups),
	}
	for _, g := range groups {
		if g.HasFlags {
			summary.FlaggedGroupCount++
		}
	}

	flagCounts := map[string]int{}
	flagSeverity := map[string]string{}
	for _, ix := range rows {
		if ix.StartedAt.After(summary.LatestEvent) {
			summary.LatestEvent = ix.StartedAt
		}
		for _, code := range splitFlagCodes(ix.FlagCodes) {
			severity := severityForFlagCode(code)
			switch severity {
			case "high":
				summary.Severity.High++
			case "medium":
				summary.Severity.Medium++
			case "low":
				summary.Severity.Low++
			case "info":
				summary.Severity.Info++
			default:
				summary.Severity.Unknown++
			}
			flagCounts[code]++
			flagSeverity[code] = maxSeverity(flagSeverity[code], severity)
		}
	}

	summary.TopFlags = make([]flagSummary, 0, len(flagCounts))
	for code, count := range flagCounts {
		summary.TopFlags = append(summary.TopFlags, flagSummary{
			Code:     code,
			Severity: flagSeverity[code],
			Count:    count,
		})
	}
	sort.Slice(summary.TopFlags, func(i, j int) bool {
		if summary.TopFlags[i].Count == summary.TopFlags[j].Count {
			return summary.TopFlags[i].Code < summary.TopFlags[j].Code
		}
		return summary.TopFlags[i].Count > summary.TopFlags[j].Count
	})
	if len(summary.TopFlags) > 5 {
		summary.TopFlags = summary.TopFlags[:5]
	}
	if !summary.LatestEvent.IsZero() {
		summary.LatestEventLabel = summary.LatestEvent.Format("2006-01-02 15:04:05")
	}
	return summary
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
			for _, code := range splitFlagCodes(ix.FlagCodes) {
				g.HasFlags = true
				g.FlagCount++
				g.Severity = maxSeverity(g.Severity, severityForFlagCode(code))
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
			"Summary":  buildDashboardSummary(rows, out),
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

type sessionDetailSummary struct {
	EventCount       int
	FlagCount        int
	LatestEvent      time.Time
	LatestEventLabel string
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
			codes := splitFlagCodes(ix.FlagCodes)
			events = append(events, eventRow{
				ID: ix.ID, StartedAt: ix.StartedAt, Method: ix.Method,
				Host: normalizeHost(ix.Host), Path: ix.Path, Status: ix.Status, FlagCodes: codes,
			})
		}
		summary := sessionDetailSummary{EventCount: len(events)}
		for _, ev := range events {
			if ev.StartedAt.After(summary.LatestEvent) {
				summary.LatestEvent = ev.StartedAt
			}
			summary.FlagCount += len(ev.FlagCodes)
		}
		if !summary.LatestEvent.IsZero() {
			summary.LatestEventLabel = summary.LatestEvent.Format("2006-01-02 15:04:05")
		}
		r.Render(w, req, "session_detail", map[string]any{
			"SessionID": sid,
			"Label":     label,
			"Events":    events,
			"Summary":   summary,
		})
	}
}

type eventDetail struct {
	ID              string
	SessionID       string
	Method          string
	URL             string
	Host            string
	Status          int
	StartedAt       time.Time
	ReqHeaders      http.Header
	RespHeaders     http.Header
	ReqBody         template.HTML
	RespBody        template.HTML
	ReqPII          []PIICount
	RespPII         []PIICount
	Flags           []types.Flag
	Raw             bool
	CaptureMode     string
	HostTrusted     bool
	HostBlocked     bool
	HostPassthrough bool
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
		reqBodyStr = formatBody(reqBodyStr, stored.ReqHeaders)
		respBodyStr = formatBody(respBodyStr, stored.RespHeaders)
		reqMatches := pii.Find([]byte(reqBodyStr), pii.DetectKind(stored.ReqHeaders))
		respMatches := pii.Find([]byte(respBodyStr), pii.DetectKind(stored.RespHeaders))
		reqBodyHTML := highlightBody(reqBodyStr, stored.ReqHeaders, reqMatches)
		respBodyHTML := highlightBody(respBodyStr, stored.RespHeaders, respMatches)
		reqPII := SummarizePII(reqMatches)
		respPII := SummarizePII(respMatches)

		host := normalizeHost(ix.Host)
		blocked := false
		if opts.Denylist != nil {
			blocked = opts.Denylist.Contains(host)
		}
		passthroughed := false
		if opts.Passthrough != nil {
			passthroughed = opts.Passthrough.Contains(host)
		}
		detail := eventDetail{
			ID: id, SessionID: ix.SessionID, Method: ix.Method, URL: stored.URL,
			Host:   host,
			Status: ix.Status, StartedAt: ix.StartedAt,
			ReqHeaders:  redactor.RedactHeaders(stored.ReqHeaders),
			RespHeaders: redactor.RedactHeaders(stored.RespHeaders),
			ReqBody:     reqBodyHTML, RespBody: respBodyHTML,
			ReqPII: reqPII, RespPII: respPII,
			Flags: stored.Flags, Raw: raw,
			CaptureMode:     ix.CaptureMode,
			HostTrusted:     opts.Allowlist.Contains(host),
			HostBlocked:     blocked,
			HostPassthrough: passthroughed,
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

func handleBlock(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if opts.Denylist == nil {
			http.Error(w, "denylist not available (supervisor wired without it)", 503)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		host := req.FormValue("host")
		if err := opts.Denylist.Add(host); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<span class="block-banner">blocked ` + html.EscapeString(host) + `</span>`))
	}
}

func handlePassthrough(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if opts.Passthrough == nil {
			http.Error(w, "passthrough list not available (supervisor wired without it)", 503)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		host := req.FormValue("host")
		if err := opts.Passthrough.Add(host); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<span class="passthrough-banner">passthrough ` + html.EscapeString(host) + ` (restart agent-gate run for it to take effect)</span>`))
	}
}

func handleUntrust(opts Options) http.HandlerFunc {
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
		if err := opts.Allowlist.Remove(host); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<span class="warn">untrusted ` + html.EscapeString(host) + `</span>`))
	}
}
