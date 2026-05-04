package initwizard

import (
	"strings"

	"github.com/charmbracelet/huh"
)

// HuhPrompter implements Prompter using charmbracelet/huh standalone forms.
type HuhPrompter struct{}

func (HuhPrompter) PromptHosts(suggested []string) ([]string, error) {
	if len(suggested) == 0 {
		return nil, nil
	}
	options := make([]huh.Option[string], 0, len(suggested))
	for _, h := range suggested {
		options = append(options, huh.NewOption(h, h).Selected(true))
	}
	var selected []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Trust these upstream hosts by default?").
			Description("Trusted hosts pass without flags. Adjust later from the dashboard.").
			Options(options...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		return nil, err
	}
	return selected, nil
}

func (HuhPrompter) PromptCustomHosts() ([]string, error) {
	var hosts []string
	for {
		var host string
		input := huh.NewInput().
			Title("Add a custom host (leave blank to finish)").
			Placeholder("api.example.com").
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

func (HuhPrompter) PromptThreeListNote() error {
	note := huh.NewNote().Title("How agent-gate decides what to do with a host").Description(
		"  ALLOWLIST    → \"this is OK, no flag\"  (Trust hosts from the dashboard)\n" +
			"  DENYLIST     → \"block, return 403\"   (Block hosts; always wins)\n" +
			"  PASSTHROUGH  → \"tunnel raw, no MITM\" (cert-pinned upstreams; metadata only)\n" +
			"\n" +
			"Tune from http://localhost:7878 with real traffic visible.",
	)
	return note.Run()
}

func (HuhPrompter) PromptInstallCert() (bool, error) {
	var ok bool
	c := huh.NewConfirm().
		Title("Install the local CA into your system trust store?").
		Description("agent-gate cannot inspect HTTPS without it. (sudo prompt may follow)").
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
		Title("Run a quick smoke test?").
		Description("Proxies a single tiny HTTPS request to verify capture works.").
		Value(&ok)
	if err := c.Run(); err != nil {
		return false, err
	}
	return ok, nil
}
