package initwizard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
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
	promptHostsDescTemplate = "Selected hosts are still audited and visible in the dashboard,\n" +
		"just not flagged for review. Anything you don't select is still\n" +
		"allowed - it just shows up flagged the first time it's seen.\n" +
		"Change any time at http://localhost:%d."

	promptCustomHostsTitle        = "Add a custom host? (leave blank to finish)"
	promptCustomHostsPlaceholder  = "api.example.com"
	promptCustomHostsDescTemplate = "Anything you add joins the quiet set above. Skip if you're\n" +
		"not sure - you can add hosts from http://localhost:%d\n" +
		"whenever they show up."

	promptPolicySummaryTitle        = "Your starting policy:"
	promptPolicySummaryDescTemplate = "  Quiet (allowed, audited, not flagged):  %s\n" +
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
		Description(fmt.Sprintf(promptWelcomeDescTemplate, port))
	return note.Run()
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
	))
	if err := form.Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

func (HuhPrompter) PromptCustomHosts(port int) ([]string, error) {
	var hosts []string
	for {
		var host string
		input := huh.NewInput().
			Title(promptCustomHostsTitle).
			Description(fmt.Sprintf(promptCustomHostsDescTemplate, port)).
			Placeholder(promptCustomHostsPlaceholder).
			Value(&host)
		if err := input.Run(); err != nil {
			return hosts, err
		}
		host = strings.TrimSpace(host)
		if host == "" {
			return hosts, nil
		}
		hosts = append(hosts, host)
	}
}

func (HuhPrompter) PromptThreeListNote(port int) error {
	note := huh.NewNote().
		Title(promptThreeListTitle).
		Description(fmt.Sprintf(promptThreeListDescTemplate, port))
	return note.Run()
}

func (HuhPrompter) PromptPolicySummary(quietCount int, port int) error {
	note := huh.NewNote().
		Title(promptPolicySummaryTitle).
		Description(fmt.Sprintf(promptPolicySummaryDescTemplate, hostCountLabel(quietCount), port))
	return note.Run()
}

func (HuhPrompter) PromptInstallCert() (bool, error) {
	var ok bool
	c := huh.NewConfirm().
		Title(promptInstallCertTitle).
		Description(promptInstallCertDesc).
		Affirmative("Install").
		Negative("Skip").
		Value(&ok)
	if err := c.Run(); err != nil {
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
	if err := c.Run(); err != nil {
		return false, err
	}
	return ok, nil
}

func hostCountLabel(count int) string {
	if count == 1 {
		return "1 host"
	}
	return fmt.Sprintf("%d hosts", count)
}
