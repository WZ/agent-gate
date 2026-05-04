package dashboard

import (
	"bytes"
	"encoding/json"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-gate/internal/store"
	"agent-gate/internal/types"
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
	Snippet   template.HTML // populated when ?q= is set; HTML-safe (post-escape, with <mark> wrapping)
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
		if path := strings.TrimSuffix(req.URL.Path, "/"); path != "/explore" {
			http.NotFound(w, req)
			return
		}
		q := req.URL.Query()
		kinds := splitCSV(q.Get("kinds"))
		hosts := splitCSV(q.Get("host"))
		preset := q.Get("preset")
		if preset == "" {
			preset = "24h"
		}
		since, until := presetWindow(preset, time.Now())

		filter := store.QueryFilter{Limit: 500, Since: since, Until: until}
		rows, err := opts.Store.Index().Query(filter)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// kind-filter: keep only events whose event_pii contains one of `kinds`.
		if len(kinds) > 0 {
			keep, err := exploreEventsByKind(opts.Store.Index(), kinds)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			filtered := rows[:0]
			for _, ix := range rows {
				if keep[ix.ID] {
					filtered = append(filtered, ix)
				}
			}
			rows = filtered
		}

		// Compute host options from the candidate set BEFORE host filter applies,
		// so the picker shows everything in the time window — not just the
		// currently-selected host.
		hostCounts := map[string]int{}
		for _, ix := range rows {
			hostCounts[normalizeHost(ix.Host)]++
		}

		// Now apply the host filter.
		if len(hosts) > 0 {
			allowed := map[string]bool{}
			for _, h := range hosts {
				allowed[h] = true
			}
			filtered := rows[:0]
			for _, ix := range rows {
				if allowed[normalizeHost(ix.Host)] {
					filtered = append(filtered, ix)
				}
			}
			rows = filtered
		}

		qStr := strings.TrimSpace(q.Get("q"))
		var snippets map[string]template.HTML
		if qStr != "" {
			qLower := strings.ToLower(qStr)
			searched := rows[:0]
			snippets = make(map[string]template.HTML, len(rows))
			for _, ix := range rows {
				ok, snippet, err := searchEvent(opts.Store, ix.ID, qLower)
				if err != nil || !ok {
					continue
				}
				searched = append(searched, ix)
				snippets[ix.ID] = snippet
			}
			rows = searched
		}

		view := exploreView{
			ActiveKinds: kinds,
			ActiveHosts: hosts,
			Preset:      preset,
			Q:           qStr,
			Rows:        make([]exploreRow, 0, len(rows)),
			HostOptions: sortedHostOptions(hostCounts, 20),
		}
		for _, ix := range rows {
			row := exploreRow{
				ID:        ix.ID,
				StartedAt: ix.StartedAt,
				Method:    ix.Method,
				Host:      normalizeHost(ix.Host),
				Path:      ix.Path,
				Status:    ix.Status,
				FlagCodes: splitFlagCodes(ix.FlagCodes),
			}
			if s, ok := snippets[ix.ID]; ok {
				row.Snippet = s
			}
			view.Rows = append(view.Rows, row)
		}
		ids := make([]string, len(view.Rows))
		for i, r := range view.Rows {
			ids[i] = r.ID
		}
		chipsByID, err := loadPIIChipsForEvents(opts.Store.Index(), ids)
		if err == nil {
			for i := range view.Rows {
				view.Rows[i].PIICounts = chipsByID[view.Rows[i].ID]
			}
		}
		view.TotalCount = len(view.Rows)
		if view.TotalCount > 0 {
			view.LatestEvent = view.Rows[0].StartedAt.Format("2006-01-02 15:04:05")
		}
		r.Render(w, req, "explore", view)
	}
}

// presetWindow turns one of {1h, 24h, 7d, all} into a (since, until) window
// relative to now. Unknown or empty preset falls back to "24h".
func presetWindow(preset string, now time.Time) (since, until time.Time) {
	until = now
	switch preset {
	case "1h":
		since = now.Add(-1 * time.Hour)
	case "24h", "":
		since = now.Add(-24 * time.Hour)
	case "7d":
		since = now.Add(-7 * 24 * time.Hour)
	case "all":
		// since stays zero → no lower bound.
	default:
		since = now.Add(-24 * time.Hour)
	}
	return since, until
}

// presetOption is one entry in the time-preset chip strip.
type presetOption struct {
	Value, Label string
}

func presetOptions() []presetOption {
	return []presetOption{
		{"1h", "Last hour"},
		{"24h", "Last 24h"},
		{"7d", "Last 7 days"},
		{"all", "All time"},
	}
}

// splitCSV splits a comma-separated list, trimming whitespace and dropping empties.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// exploreEventsByKind returns the set of event_ids whose event_pii row
// contains at least one row matching the requested kinds.
func exploreEventsByKind(idx *store.Index, kinds []string) (map[string]bool, error) {
	placeholders := make([]string, len(kinds))
	args := make([]any, len(kinds))
	for i, k := range kinds {
		placeholders[i] = "?"
		args[i] = k
	}
	q := `SELECT DISTINCT event_id FROM event_pii WHERE code IN (` +
		strings.Join(placeholders, ",") + `)`
	rows, err := idx.Db().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keep := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		keep[id] = true
	}
	return keep, rows.Err()
}

// piiKindOption is one entry in the kind-chip strip; ordered sensitive-first
// then by Code for stable rendering.
type piiKindOption struct {
	Code, Label, Tier string
}

func piiKindOptions() []piiKindOption {
	codes := []struct{ code, label, tier string }{
		{"ssn", "SSN", "sensitive"},
		{"credit_card", "Credit card", "sensitive"},
		{"dob", "DOB", "sensitive"},
		{"email", "Email", "identifying"},
		{"phone", "Phone", "identifying"},
		{"name", "Name", "identifying"},
		{"address", "Address", "identifying"},
		{"jwt", "JWT", "identifying"},
		{"uuid", "UUID", "identifying"},
		{"ipv4", "IPv4", "identifying"},
	}
	out := make([]piiKindOption, len(codes))
	for i, c := range codes {
		out[i] = piiKindOption{Code: c.code, Label: c.label, Tier: c.tier}
	}
	return out
}

func hasKind(active []string, code string) bool {
	for _, a := range active {
		if a == code {
			return true
		}
	}
	return false
}

func sortedHostOptions(counts map[string]int, max int) []hostOption {
	out := make([]hostOption, 0, len(counts))
	for h, n := range counts {
		out = append(out, hostOption{Name: h, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// toggleStringInList flips a value in or out of active and returns the
// resulting slice (nil-safe). Used by the toggle URL helpers below.
func toggleStringInList(active []string, value string) []string {
	next := make([]string, 0, len(active)+1)
	found := false
	for _, a := range active {
		if a == value {
			found = true
			continue
		}
		next = append(next, a)
	}
	if !found {
		next = append(next, value)
	}
	return next
}

// filterURL renders the /explore querystring for the current view with one
// filter overridden. Use it everywhere a link should preserve filter state
// across navigation. `field` is one of: "kinds", "host", "preset", "q",
// "page". `value` is a slice (kinds/host) or single string formatted as
// the param value. Empty result means an empty querystring.
func filterURL(view exploreView, field string, override interface{}) string {
	parts := []string{}
	emit := func(name, value string) {
		if value != "" {
			parts = append(parts, name+"="+value)
		}
	}
	kinds := strings.Join(view.ActiveKinds, ",")
	hosts := strings.Join(view.ActiveHosts, ",")
	preset := view.Preset
	q := view.Q
	page := 0 // 0 means "omit" → defaults to 1 on the server
	switch field {
	case "kinds":
		kinds = strings.Join(toggleStringInList(view.ActiveKinds, override.(string)), ",")
	case "host":
		hosts = strings.Join(toggleStringInList(view.ActiveHosts, override.(string)), ",")
	case "preset":
		preset = override.(string)
	case "q":
		q = override.(string)
	case "page":
		page = override.(int)
	}
	emit("q", urlQueryEscape(q))
	emit("kinds", kinds)
	emit("host", hosts)
	emit("preset", preset)
	if page > 1 {
		emit("page", strconv.Itoa(page))
	}
	return strings.Join(parts, "&")
}

// urlQueryEscape is a thin wrapper so template callers don't need
// to import net/url.
func urlQueryEscape(s string) string { return url.QueryEscape(s) }

// snippetMaxContext bytes on each side of a hit.
const snippetMaxContext = 60

// snippetMaxHitsPerEvent is the maximum number of snippets we render per row.
const snippetMaxHitsPerEvent = 3

// searchEvent loads an event's stored bodies and url, runs a case-insensitive
// substring scan, and returns true plus an HTML-safe snippet HTML when q
// occurs. q is assumed lowercased by the caller.
func searchEvent(s *store.Store, eventID, qLower string) (matched bool, snippet template.HTML, err error) {
	rdr, err := s.Body(eventID)
	if err != nil {
		return false, "", err
	}
	defer rdr.Close()
	raw, err := io.ReadAll(rdr)
	if err != nil {
		return false, "", err
	}
	var ev types.StoredEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return false, "", err
	}
	// haystacks: url + req body + resp body. headers excluded by spec
	// decision (noisy bearer tokens dominate signal).
	for _, hay := range [][]byte{[]byte(ev.URL), ev.ReqBody, ev.RespBody} {
		if bytes.Contains(bytes.ToLower(hay), []byte(qLower)) {
			matched = true
			break
		}
	}
	if !matched {
		return false, "", nil
	}
	// Build snippet HTML from the request body if it has the hit; else
	// the response body; else the URL.
	for _, hay := range [][]byte{ev.ReqBody, ev.RespBody, []byte(ev.URL)} {
		if bytes.Contains(bytes.ToLower(hay), []byte(qLower)) {
			snippet = renderSnippet(hay, qLower)
			break
		}
	}
	return true, snippet, nil
}

// renderSnippet wraps every hit of qLower (case-insensitive) in <mark>,
// taking up to snippetMaxHitsPerEvent occurrences with snippetMaxContext
// bytes of surrounding context. The result is HTML-escaped before <mark>
// insertion so payload bytes can never break the wrapper.
func renderSnippet(body []byte, qLower string) template.HTML {
	if len(qLower) == 0 || len(body) == 0 {
		return ""
	}
	low := bytes.ToLower(body)
	hits := 0
	var out bytes.Buffer
	cursor := 0
	for cursor < len(low) && hits < snippetMaxHitsPerEvent {
		idx := bytes.Index(low[cursor:], []byte(qLower))
		if idx < 0 {
			break
		}
		hitStart := cursor + idx
		hitEnd := hitStart + len(qLower)
		from := hitStart - snippetMaxContext
		if from < 0 {
			from = 0
		}
		to := hitEnd + snippetMaxContext
		if to > len(body) {
			to = len(body)
		}
		if hits > 0 {
			out.WriteString(" … ")
		}
		out.WriteString(html.EscapeString(string(body[from:hitStart])))
		out.WriteString("<mark>")
		out.WriteString(html.EscapeString(string(body[hitStart:hitEnd])))
		out.WriteString("</mark>")
		out.WriteString(html.EscapeString(string(body[hitEnd:to])))
		cursor = hitEnd
		hits++
	}
	return template.HTML(out.String()) //nolint:gosec // body bytes html-escaped above
}
