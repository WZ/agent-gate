package dashboard

import (
	"net/http"
	"net/url"
	"strconv"
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
		if path := strings.TrimSuffix(req.URL.Path, "/"); path != "/explore" {
			http.NotFound(w, req)
			return
		}
		q := req.URL.Query()
		kinds := splitCSV(q.Get("kinds"))

		rows, err := opts.Store.Index().Query(store.QueryFilter{Limit: 500})
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

		view := exploreView{
			ActiveKinds: kinds,
			Rows:        make([]exploreRow, 0, len(rows)),
		}
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
