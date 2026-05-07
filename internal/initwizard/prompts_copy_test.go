package initwizard

import (
	"fmt"
	"strings"
	"testing"
)

func renderedPromptCopy(port int) map[string]string {
	return map[string]string{
		"welcome title":                    promptWelcomeTitle,
		"welcome description":              fmt.Sprintf(promptWelcomeDescTemplate, port),
		"three-list title":                 promptThreeListTitle,
		"three-list description":           fmt.Sprintf(promptThreeListDescTemplate, port),
		"hosts title":                      promptHostsTitle,
		"hosts description":                fmt.Sprintf(promptHostsDescTemplate, port),
		"custom hosts confirm title first": promptCustomHostsConfirmTitleFirst,
		"custom hosts confirm title again": promptCustomHostsConfirmTitleAgain,
		"custom hosts confirm description": fmt.Sprintf(promptCustomHostsConfirmDesc, port),
		"custom hosts input title":         promptCustomHostsInputTitle,
		"custom hosts placeholder":         promptCustomHostsPlaceholder,
		"hosts confirm title any":          promptHostsConfirmTitleAny,
		"hosts confirm title none":         promptHostsConfirmTitleNone,
		"hosts confirm desc none":          promptHostsConfirmDescNone,
		"policy summary title":             promptPolicySummaryTitle,
		"policy summary description":       policySummaryDescription([]string{"a.example.com", "b.example.com", "c.example.com"}, port),
		"install cert title":               promptInstallCertTitle,
		"install cert description":         promptInstallCertDesc,
		"smoke test title":                 promptSmokeTestTitle,
		"smoke test description":           promptSmokeTestDesc,
	}
}

func dashboardPromptDescriptions(port int) map[string]string {
	return map[string]string{
		"welcome":        fmt.Sprintf(promptWelcomeDescTemplate, port),
		"three-list":     fmt.Sprintf(promptThreeListDescTemplate, port),
		"hosts":          fmt.Sprintf(promptHostsDescTemplate, port),
		"custom hosts":   fmt.Sprintf(promptCustomHostsConfirmDesc, port),
		"policy summary": policySummaryDescription([]string{"a.example.com"}, port),
	}
}

func TestCopy_NoTrustWord(t *testing.T) {
	for name, text := range renderedPromptCopy(7878) {
		normalized := strings.ToLower(text)
		normalized = strings.ReplaceAll(normalized, "trust store", "")
		if strings.Contains(normalized, "trust") {
			t.Errorf("%s contains trust wording: %q", name, text)
		}
	}
}

func TestCopy_DashboardURLMentioned(t *testing.T) {
	const want = "http://localhost:7878"
	for name, text := range dashboardPromptDescriptions(7878) {
		if !strings.Contains(text, want) {
			t.Errorf("%s description missing %q: %q", name, want, text)
		}
	}
}

func TestCopy_PortSubstitutionWorks(t *testing.T) {
	const want = "http://localhost:9000"
	for name, text := range dashboardPromptDescriptions(9000) {
		if !strings.Contains(text, want) {
			t.Errorf("%s description missing %q: %q", name, want, text)
		}
		if strings.Contains(text, "http://localhost:7878") {
			t.Errorf("%s description kept default dashboard port: %q", name, text)
		}
	}
}

func TestCustomHostsConfirmText_FirstIteration_NoTally(t *testing.T) {
	title, desc := customHostsConfirmText(7878, nil)
	if title != promptCustomHostsConfirmTitleFirst {
		t.Errorf("first-iteration title: got %q, want %q", title, promptCustomHostsConfirmTitleFirst)
	}
	if strings.Contains(desc, "Added so far") {
		t.Errorf("first-iteration description should not show tally; got: %q", desc)
	}
}

func TestCustomHostsConfirmText_SubsequentIteration_ShowsTally(t *testing.T) {
	title, desc := customHostsConfirmText(7878, []string{"foo.com", "bar.com"})
	if title != promptCustomHostsConfirmTitleAgain {
		t.Errorf("followup title: got %q, want %q", title, promptCustomHostsConfirmTitleAgain)
	}
	for _, want := range []string{"Added so far:", "foo.com", "bar.com"} {
		if !strings.Contains(desc, want) {
			t.Errorf("followup description missing %q; got: %q", want, desc)
		}
	}
}

func TestPolicySummaryDescription_ListsEachHost(t *testing.T) {
	desc := policySummaryDescription([]string{"api.anthropic.com", "api.openai.com", "chatgpt.com"}, 7878)
	for _, want := range []string{
		"3 hosts",
		"api.anthropic.com",
		"api.openai.com",
		"chatgpt.com",
		"http://localhost:7878",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("policy summary missing %q; got:\n%s", want, desc)
		}
	}
}

func TestPolicySummaryDescription_EmptyHosts_NoListedNames(t *testing.T) {
	desc := policySummaryDescription(nil, 7878)
	if !strings.Contains(desc, "0 hosts") {
		t.Errorf("policy summary should show '0 hosts'; got:\n%s", desc)
	}
	// With zero hosts, the line that would carry indented names must be
	// blank — no leading "    " before a non-space character.
	for line := range strings.SplitSeq(desc, "\n") {
		if strings.HasPrefix(line, "    ") && strings.TrimSpace(line) != "" {
			t.Errorf("policy summary with no hosts contains indented host line %q; full:\n%s", line, desc)
		}
	}
}

func TestTheme_ButtonsDoNotUseDefaultPink(t *testing.T) {
	got := fmt.Sprint(initWizardTheme().Focused.FocusedButton.GetBackground())
	if got == "#F780E2" {
		t.Fatalf("focused button background uses default huh pink/fuchsia")
	}
	if got != "#0E7C5A" {
		t.Fatalf("focused button background = %q, want #0E7C5A", got)
	}
}
