package ca

// MockInstaller is a test double for Installer. Records calls; returns the
// configured errors. Compiled into the binary as inert (no test build tag)
// so it's also available for ad-hoc dogfood scripts.
type MockInstaller struct {
	InstallCalls   []string
	UninstallCalls []string
	InstallErr     error
	UninstallErr   error
	ProbeResults   []StoreProbe // if nil, ProbeAll fabricates one Present per InstallCalls
}

func (m *MockInstaller) InstallFile(certPath string) error {
	m.InstallCalls = append(m.InstallCalls, certPath)
	return m.InstallErr
}

func (m *MockInstaller) UninstallFile(certPath string) error {
	m.UninstallCalls = append(m.UninstallCalls, certPath)
	return m.UninstallErr
}

func (m *MockInstaller) ProbeAll(certPath string) []StoreProbe {
	if m.ProbeResults != nil {
		return m.ProbeResults
	}
	if len(m.InstallCalls) == 0 {
		return []StoreProbe{{Store: "mock", Present: false, Note: "no install recorded"}}
	}
	return []StoreProbe{{Store: "mock", Present: true, Note: "install recorded"}}
}
