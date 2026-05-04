package doctor

import (
	"encoding/json"
	"fmt"
	"io"
)

type Report struct {
	Results []Result
}

func (rep Report) WriteHuman(w io.Writer) {
	fmt.Fprintln(w, "agent-gate doctor")
	fmt.Fprintln(w, "─────────────────")
	var ok, warn, fail, skip int
	for _, r := range rep.Results {
		fmt.Fprintf(w, "%s  %-26s %s\n", r.Status.Glyph(), r.ID, r.Detail)
		switch r.Status {
		case StatusOK:
			ok++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		case StatusSkip:
			skip++
		}
	}
	fmt.Fprintf(w, "\n%d failed, %d warnings, %d skipped, %d OK.\n", fail, warn, skip, ok)
	if fail > 0 || warn > 0 {
		fmt.Fprintln(w, "Fix:")
		for _, r := range rep.Results {
			if r.FixHint != "" && (r.Status == StatusFail || r.Status == StatusWarn) {
				fmt.Fprintf(w, "  %-26s →  %s\n", r.ID, r.FixHint)
			}
		}
	}
}

func (rep Report) WriteJSON(w io.Writer) error {
	type wireResult struct {
		ID      string `json:"id"`
		Status  string `json:"status"`
		Detail  string `json:"detail"`
		FixHint string `json:"fix_hint,omitempty"`
	}
	statusName := func(s Status) string {
		switch s {
		case StatusOK:
			return "ok"
		case StatusWarn:
			return "warn"
		case StatusFail:
			return "fail"
		case StatusSkip:
			return "skip"
		}
		return "unknown"
	}
	out := struct {
		Results []wireResult `json:"results"`
	}{}
	for _, r := range rep.Results {
		out.Results = append(out.Results, wireResult{
			ID: r.ID, Status: statusName(r.Status), Detail: r.Detail, FixHint: r.FixHint,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
