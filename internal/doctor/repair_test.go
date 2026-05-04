package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"agent-gate/internal/ca"
)

func TestRepair_SafeNeverMutatesTrustStore(t *testing.T) {
	mock := &ca.MockInstaller{}
	dir := t.TempDir()
	results := []Result{
		{ID: "ca-trusted-macos-keychain", Status: StatusFail, FixHint: "agent-gate cert install"},
	}
	r := Repair(RepairOpts{Mode: ModeSafe, Installer: mock, ConfigDir: dir})
	r.Apply(results)
	if len(mock.InstallCalls) != 0 {
		t.Fatalf("safe mode must not call InstallFile, got %d calls", len(mock.InstallCalls))
	}
}

func TestRepair_SafeFixesFilesystemPerms(t *testing.T) {
	if isWindows() {
		t.Skip()
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "data-dir-test")
	_ = os.Mkdir(bad, 0o755)
	results := []Result{
		{ID: "data-dir", Status: StatusWarn, FixHint: "chmod 0700 " + bad},
	}
	r := Repair(RepairOpts{Mode: ModeSafe, ConfigDir: dir})
	r.Apply(results)
	info, _ := os.Stat(bad)
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("safe-mode should chmod own dirs, got mode %o", info.Mode().Perm())
	}
}

func TestRepair_AggressiveCallsInstaller(t *testing.T) {
	mock := &ca.MockInstaller{}
	dir := t.TempDir()
	cert := filepath.Join(dir, "ca", "cert.pem")
	_ = os.MkdirAll(filepath.Dir(cert), 0o700)
	_ = os.WriteFile(cert, []byte("CERT"), 0o644)
	results := []Result{
		{ID: "ca-trusted-macos-keychain", Status: StatusFail, FixHint: "agent-gate cert install"},
	}
	r := Repair(RepairOpts{Mode: ModeAggressive, Installer: mock, ConfigDir: dir, CertPath: cert})
	r.Apply(results)
	if len(mock.InstallCalls) != 1 {
		t.Fatalf("aggressive mode should call InstallFile once, got %d", len(mock.InstallCalls))
	}
}
