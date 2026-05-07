package initwizard

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintBannerContainsWordmarkAndTagline(t *testing.T) {
	var buf bytes.Buffer
	PrintBanner(&buf, "")
	got := buf.String()

	for _, line := range bannerArt {
		if !strings.Contains(got, line) {
			t.Errorf("banner missing wordmark line %q\nfull output:\n%s", line, got)
		}
	}
	if !strings.Contains(got, bannerTagline) {
		t.Errorf("banner missing tagline %q\nfull output:\n%s", bannerTagline, got)
	}
}

func TestPrintBannerIncludesVersionWhenProvided(t *testing.T) {
	var buf bytes.Buffer
	PrintBanner(&buf, "v0.3.1")
	got := buf.String()

	if !strings.Contains(got, "v0.3.1") {
		t.Errorf("banner did not include version; output:\n%s", got)
	}
}

func TestPrintBannerOmitsVersionWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	PrintBanner(&buf, "")
	if strings.Contains(buf.String(), " · ") {
		t.Errorf("banner should not include separator when version is empty; output:\n%s", buf.String())
	}
}
