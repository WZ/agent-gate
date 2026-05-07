package initwizard

import (
	"fmt"
	"strings"
	"testing"
)

func renderedPromptCopy(port int) map[string]string {
	return map[string]string{
		"welcome title":              promptWelcomeTitle,
		"welcome description":        fmt.Sprintf(promptWelcomeDescTemplate, port),
		"three-list title":           promptThreeListTitle,
		"three-list description":     fmt.Sprintf(promptThreeListDescTemplate, port),
		"hosts title":                promptHostsTitle,
		"hosts description":          fmt.Sprintf(promptHostsDescTemplate, port),
		"custom hosts title":         promptCustomHostsTitle,
		"custom hosts title again":   promptCustomHostsTitleAgain,
		"custom hosts placeholder":   promptCustomHostsPlaceholder,
		"custom hosts description":   fmt.Sprintf(promptCustomHostsDescTemplate, port),
		"hosts confirm title any":    promptHostsConfirmTitleAny,
		"hosts confirm title none":   promptHostsConfirmTitleNone,
		"hosts confirm desc none":    promptHostsConfirmDescNone,
		"policy summary title":       promptPolicySummaryTitle,
		"policy summary description": fmt.Sprintf(promptPolicySummaryDescTemplate, "3 hosts", port),
		"install cert title":         promptInstallCertTitle,
		"install cert description":   promptInstallCertDesc,
		"smoke test title":           promptSmokeTestTitle,
		"smoke test description":     promptSmokeTestDesc,
	}
}

func dashboardPromptDescriptions(port int) map[string]string {
	return map[string]string{
		"welcome":        fmt.Sprintf(promptWelcomeDescTemplate, port),
		"three-list":     fmt.Sprintf(promptThreeListDescTemplate, port),
		"hosts":          fmt.Sprintf(promptHostsDescTemplate, port),
		"custom hosts":   fmt.Sprintf(promptCustomHostsDescTemplate, port),
		"policy summary": fmt.Sprintf(promptPolicySummaryDescTemplate, "3 hosts", port),
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

func TestCustomHostsPromptText_FirstIteration_NoTally(t *testing.T) {
	title, desc := customHostsPromptText(7878, nil)
	if title != promptCustomHostsTitle {
		t.Errorf("first-iteration title: got %q, want %q", title, promptCustomHostsTitle)
	}
	if strings.Contains(desc, "Added so far") {
		t.Errorf("first-iteration description should not show tally; got: %q", desc)
	}
}

func TestCustomHostsPromptText_SubsequentIteration_ShowsTally(t *testing.T) {
	title, desc := customHostsPromptText(7878, []string{"foo.com", "bar.com"})
	if title != promptCustomHostsTitleAgain {
		t.Errorf("followup title: got %q, want %q", title, promptCustomHostsTitleAgain)
	}
	for _, want := range []string{"Added so far:", "foo.com", "bar.com"} {
		if !strings.Contains(desc, want) {
			t.Errorf("followup description missing %q; got: %q", want, desc)
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
