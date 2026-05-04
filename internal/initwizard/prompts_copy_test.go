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
		"custom hosts placeholder":   promptCustomHostsPlaceholder,
		"custom hosts description":   fmt.Sprintf(promptCustomHostsDescTemplate, port),
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
