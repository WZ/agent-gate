package initwizard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

const (
	promptWelcomeTitle        = "Welcome to agent-gate."
	promptWelcomeDescTemplate = "agent-gate puts a local review gate between AI agents\n" +
		"(claude, codex, aider, gh, ...) and the internet. It\n" +
		"intercepts outbound HTTPS through a local proxy, records\n" +
		"each request, flags anything worth review, and shows it\n" +
		"all in a dashboard at http://localhost:%d.\n\n" +
		"Nothing leaves your machine. Every choice in this wizard\n" +
		"can be changed later from the dashboard."

	promptThreeListTitle        = "How agent-gate handles hosts"
	promptThreeListDescTemplate = "agent-gate decides what to do with each host using three lists:\n\n" +
		"  ALLOWLIST    allowed and not flagged for review\n" +
		"               (the quiet set you'll pick on the next screen)\n\n" +
		"  DENYLIST     blocked at the proxy with a 403\n" +
		"               (always wins; you can add hosts here later)\n\n" +
		"  PASSTHROUGH  allowed without HTTPS inspection\n" +
		"               (for cert-pinned upstreams; metadata only)\n\n" +
		"Every other host is allowed, audited, logged, and flagged\n" +
		"for review. Tune any of this from http://localhost:%d\n" +
		"once real traffic is visible."

	promptHostsTitle        = "Start these hosts quiet?"
	promptHostsDescTemplate = "Press SPACE to toggle a host, ENTER to confirm.\n\n" +
		"Selected hosts are still audited and visible in the dashboard,\n" +
		"just not flagged for review. Anything you don't select is still\n" +
		"allowed - it just shows up flagged the first time it's seen.\n" +
		"Change any time at http://localhost:%d."

	promptHostsConfirmTitleAny  = "Mark these hosts quiet?"
	promptHostsConfirmTitleNone = "Start with no quiet hosts?"
	promptHostsConfirmDescNone  = "You'll see each host flagged the first time it appears\n" +
		"in the dashboard, where you can mark it quiet from there."

	promptCustomHostsConfirmTitleFirst = "Add a custom host?"
	promptCustomHostsConfirmTitleAgain = "Add another?"
	promptCustomHostsConfirmDesc       = "Anything you add joins the set you just picked. Skip if\n" +
		"you're not sure - you can add hosts from http://localhost:%d\n" +
		"whenever you spot one in the dashboard."
	promptCustomHostsAddedPrefix = "Added so far: %s\n\n"

	promptCustomHostsInputTitle  = "Type a hostname (Enter to add):"
	promptCustomHostsPlaceholder = "api.example.com"

	promptPolicySummaryTitle        = "Your starting policy:"
	promptPolicySummaryDescTemplate = "  Quiet (allowed, audited, not flagged):  %s\n" +
		"%s" +
		"  Blocked (403 at the proxy):              0 hosts\n" +
		"  Passthrough (no HTTPS inspection):       0 hosts\n\n" +
		"  Everything else: allowed, audited, and flagged for review.\n\n" +
		"Visit http://localhost:%d once you've started capturing\n" +
		"to adjust any of this against real traffic."

	promptInstallCertTitle = "Install the local CA into your system trust store?"
	promptInstallCertDesc  = "agent-gate cannot inspect HTTPS without it.\n" +
		"A sudo prompt may follow. You can remove the CA\n" +
		"later with `agent-gate cert uninstall`."

	promptSmokeTestTitle = "Run a quick smoke test?"
	promptSmokeTestDesc  = "Proxies a single tiny HTTPS request to verify capture works."
)

// HuhPrompter implements Prompter using charmbracelet/huh standalone forms.
type HuhPrompter struct{}

func (HuhPrompter) PromptWelcome(port int) error {
	note := huh.NewNote().
		Title(promptWelcomeTitle).
		Description(fmt.Sprintf(promptWelcomeDescTemplate, port)).
		Next(true).
		NextLabel("Continue")
	return huh.NewForm(huh.NewGroup(note)).
		WithTheme(initWizardTheme()).
		Run()
}

func (HuhPrompter) PromptHosts(suggested []HostSuggestion, port int) ([]string, error) {
	if len(suggested) == 0 {
		return nil, nil
	}
	options := make([]huh.Option[string], 0, len(suggested))
	for _, suggestion := range suggested {
		label := suggestion.Host
		if len(suggestion.Agents) > 0 {
			label = fmt.Sprintf("%s  (%s)", suggestion.Host, strings.Join(suggestion.Agents, ", "))
		}
		options = append(options, huh.NewOption(label, suggestion.Host).Selected(true))
	}
	var selected []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title(promptHostsTitle).
			Description(fmt.Sprintf(promptHostsDescTemplate, port)).
			Options(options...).
			Value(&selected),
	)).WithTheme(initWizardTheme())
	if err := form.Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

// PromptHostsConfirm asks the user to confirm the selection from PromptHosts.
// Returning false re-runs the multiselect; true advances. The list of hosts
// is shown so the user can verify what they're committing to.
func (HuhPrompter) PromptHostsConfirm(selected []string) (bool, error) {
	title := promptHostsConfirmTitleAny
	desc := strings.Join(selected, "\n")
	if len(selected) == 0 {
		title = promptHostsConfirmTitleNone
		desc = promptHostsConfirmDescNone
	}
	// Start with ok=true so the affirmative button is the default focus —
	// "Continue with this set" is the common path; "Back" is the rare one.
	ok := true
	c := huh.NewConfirm().
		Title(title).
		Description(desc).
		Affirmative("Yes, continue").
		Negative("Back, edit selection").
		Value(&ok)
	if err := huh.NewForm(huh.NewGroup(c)).
		WithTheme(initWizardTheme()).
		Run(); err != nil {
		return false, err
	}
	return ok, nil
}

// customHostsConfirmText returns the title + description for the Y/N gate
// before each custom-host input. Pure (no TTY) so it can be tested directly.
//
// First iteration (alreadyAdded empty): "Add a custom host?" with the
// general description. Subsequent iterations: "Add another?" with a
// "Added so far: foo, bar" line above the description so the user
// sees their previous adds confirmed before deciding to keep going.
func customHostsConfirmText(port int, alreadyAdded []string) (title, desc string) {
	desc = fmt.Sprintf(promptCustomHostsConfirmDesc, port)
	if len(alreadyAdded) == 0 {
		return promptCustomHostsConfirmTitleFirst, desc
	}
	tally := fmt.Sprintf(promptCustomHostsAddedPrefix, strings.Join(alreadyAdded, ", "))
	return promptCustomHostsConfirmTitleAgain, tally + desc
}

// PromptCustomHosts runs a Y/N gate before each input box so users
// don't have to discover that "leave blank to finish" is the way out.
// "No" exits the loop; "Yes" opens an input box; submitting that input
// (blank or not) returns to the Y/N gate.
func (HuhPrompter) PromptCustomHosts(port int) ([]string, error) {
	var hosts []string
	for {
		title, desc := customHostsConfirmText(port, hosts)
		// Default focus on "No" — most users skip this step entirely,
		// and accidental Enter from the previous prompt shouldn't drop
		// them into a textbox they didn't ask for.
		addMore := false
		c := huh.NewConfirm().
			Title(title).
			Description(desc).
			Affirmative("Yes, add one").
			Negative("No, continue").
			Value(&addMore)
		if err := huh.NewForm(huh.NewGroup(c)).
			WithTheme(initWizardTheme()).
			Run(); err != nil {
			return hosts, err
		}
		if !addMore {
			return hosts, nil
		}

		var host string
		input := huh.NewInput().
			Title(promptCustomHostsInputTitle).
			Placeholder(promptCustomHostsPlaceholder).
			Value(&host)
		if err := huh.NewForm(huh.NewGroup(input)).
			WithTheme(initWizardTheme()).
			Run(); err != nil {
			return hosts, err
		}
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		hosts = append(hosts, host)
	}
}

// backableNote runs a note-style page that has both Continue and Back
// buttons. Returns nil on Continue, ErrPromptBack on Back. Default focus
// is Continue, since "go forward" is the common path.
func backableNote(title, desc, continueLabel string) error {
	goForward := true
	c := huh.NewConfirm().
		Title(title).
		Description(desc).
		Affirmative(continueLabel).
		Negative("Back").
		Value(&goForward)
	if err := huh.NewForm(huh.NewGroup(c)).
		WithTheme(initWizardTheme()).
		Run(); err != nil {
		return err
	}
	if !goForward {
		return ErrPromptBack
	}
	return nil
}

func (HuhPrompter) PromptThreeListNote(port int) error {
	return backableNote(
		promptThreeListTitle,
		fmt.Sprintf(promptThreeListDescTemplate, port),
		"Continue",
	)
}

// policySummaryDescription renders the policy-summary body with the actual
// host names indented under the "Quiet" line so the user sees what's
// being committed instead of just a count. Pure (no TTY) so we can test it.
func policySummaryDescription(hosts []string, port int) string {
	var b strings.Builder
	for _, h := range hosts {
		b.WriteString("    ")
		b.WriteString(h)
		b.WriteByte('\n')
	}
	return fmt.Sprintf(promptPolicySummaryDescTemplate, hostCountLabel(len(hosts)), b.String(), port)
}

func (HuhPrompter) PromptPolicySummary(hosts []string, port int) error {
	return backableNote(
		promptPolicySummaryTitle,
		policySummaryDescription(hosts, port),
		"Continue",
	)
}

func (HuhPrompter) PromptInstallCert() (bool, error) {
	// Default focus on Install — the wizard's whole point is to set up an
	// auditable HTTPS gate, and that requires the local CA in the trust
	// store. Skipping is rare; making it the default would surprise users
	// who hit Enter expecting "yes, do the thing".
	ok := true
	c := huh.NewConfirm().
		Title(promptInstallCertTitle).
		Description(promptInstallCertDesc).
		Affirmative("Install").
		Negative("Skip").
		Value(&ok)
	if err := huh.NewForm(huh.NewGroup(c)).
		WithTheme(initWizardTheme()).
		Run(); err != nil {
		return false, err
	}
	return ok, nil
}

func (HuhPrompter) PromptSmokeTest() (bool, error) {
	var ok bool
	c := huh.NewConfirm().
		Title(promptSmokeTestTitle).
		Description(promptSmokeTestDesc).
		Value(&ok)
	if err := huh.NewForm(huh.NewGroup(c)).
		WithTheme(initWizardTheme()).
		Run(); err != nil {
		return false, err
	}
	return ok, nil
}

func initWizardTheme() *huh.Theme {
	theme := huh.ThemeCharm()
	accent := lipgloss.Color("#0E7C5A")
	buttonText := lipgloss.Color("#FFFDF5")

	theme.Focused.SelectSelector = theme.Focused.SelectSelector.Foreground(accent)
	theme.Focused.NextIndicator = theme.Focused.NextIndicator.Foreground(accent)
	theme.Focused.PrevIndicator = theme.Focused.PrevIndicator.Foreground(accent)
	theme.Focused.MultiSelectSelector = theme.Focused.MultiSelectSelector.Foreground(accent)
	theme.Focused.FocusedButton = theme.Focused.FocusedButton.Foreground(buttonText).Background(accent)
	theme.Focused.Next = theme.Focused.FocusedButton
	theme.Focused.TextInput.Prompt = theme.Focused.TextInput.Prompt.Foreground(accent)

	return theme
}

func hostCountLabel(count int) string {
	if count == 1 {
		return "1 host"
	}
	return fmt.Sprintf("%d hosts", count)
}
