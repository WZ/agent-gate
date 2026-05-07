package initwizard

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
)

// bannerArt is the wordmark printed at the top of `agent-gate init`.
// Block characters render at any reasonable terminal width (~42 cols)
// and survive copy/paste better than figlet output. Indentation is
// added by PrintBanner so lipgloss styling doesn't strip leading
// whitespace from line 1.
var bannerArt = []string{
	"▄▀█ █▀▀ █▀▀ █▄ █ ▀█▀     █▀▀ ▄▀█ ▀█▀ █▀▀",
	"█▀█ █▄█ ██▄ █ ▀█  █      █▄█ █▀█  █  ██▄",
}

const (
	bannerIndent  = "   "
	bannerTagline = "◆ local traffic audit gate"
)

// PrintBanner writes the agent-gate wordmark + tagline to w. Caller decides
// whether to suppress it (e.g. --quiet, --print-config).
//
// version is shown after the tagline; pass "" to omit. Color is applied via
// lipgloss; lipgloss auto-degrades on TERM=dumb / non-TTY writers.
func PrintBanner(w io.Writer, version string) {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#0E7C5A")).Bold(true)
	tagline := lipgloss.NewStyle().Foreground(lipgloss.Color("#0E7C5A")).Faint(true)

	fmt.Fprintln(w)
	for _, line := range bannerArt {
		fmt.Fprintln(w, bannerIndent+accent.Render(line))
	}
	line := bannerTagline
	if version != "" {
		line = fmt.Sprintf("%s · %s", bannerTagline, version)
	}
	fmt.Fprintln(w, bannerIndent+tagline.Render(line))
	fmt.Fprintln(w)
}
