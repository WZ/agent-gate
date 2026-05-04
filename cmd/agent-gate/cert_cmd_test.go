package main

import "testing"

func TestCertCmd_HasInstallUninstallPath(t *testing.T) {
	c := certCmd()
	want := map[string]bool{"install": false, "uninstall": false, "path": false}
	for _, sub := range c.Commands() {
		if _, ok := want[sub.Use]; ok {
			want[sub.Use] = true
		}
	}
	for use, found := range want {
		if !found {
			t.Errorf("cert subcommand %q missing", use)
		}
	}
}
