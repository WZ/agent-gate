package ca

import (
	"errors"
	"testing"
)

func TestMockInstaller_RecordsInstall(t *testing.T) {
	m := &MockInstaller{}
	if err := m.InstallFile("/tmp/cert.pem"); err != nil {
		t.Fatalf("InstallFile: %v", err)
	}
	if len(m.InstallCalls) != 1 || m.InstallCalls[0] != "/tmp/cert.pem" {
		t.Fatalf("expected 1 install call, got %v", m.InstallCalls)
	}
}

func TestMockInstaller_ReturnsConfiguredError(t *testing.T) {
	want := errors.New("install boom")
	m := &MockInstaller{InstallErr: want}
	if err := m.InstallFile("/tmp/cert.pem"); !errors.Is(err, want) {
		t.Fatalf("err: got %v, want %v", err, want)
	}
}

func TestMockInstaller_ProbeAll_DefaultsToInstalled(t *testing.T) {
	m := &MockInstaller{InstallCalls: []string{"/tmp/cert.pem"}}
	probes := m.ProbeAll("/tmp/cert.pem")
	if len(probes) == 0 {
		t.Fatal("expected at least one probe result")
	}
	for _, p := range probes {
		if !p.Present {
			t.Errorf("probe %s: Present=false after install (mock should report installed)", p.Store)
		}
	}
}

func TestMockInstaller_RecordsUninstall(t *testing.T) {
	m := &MockInstaller{}
	if err := m.UninstallFile("/tmp/cert.pem"); err != nil {
		t.Fatalf("UninstallFile: %v", err)
	}
	if len(m.UninstallCalls) != 1 {
		t.Fatalf("expected 1 uninstall call, got %d", len(m.UninstallCalls))
	}
}

func TestSmallstepInstaller_SatisfiesInstaller(t *testing.T) {
	var _ Installer = (*SmallstepInstaller)(nil)
	var _ Installer = SmallstepInstaller{}
}

func TestFirefoxProfiles_NeverPanics(t *testing.T) {
	// Whatever the host filesystem looks like, this should return a slice
	// without crashing.
	_ = FirefoxProfiles()
}
