package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorCmd_RunsAndPrintsReport(t *testing.T) {
	cmd := doctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	tmp := t.TempDir()
	cmd.SetArgs([]string{"--config", filepath.Join(tmp, "config.toml")})
	_ = cmd.Execute() // doctor may exit 1 (missing config + ports may bind on the test host); the report MUST print regardless.
	if !strings.Contains(buf.String(), "agent-gate doctor") {
		t.Fatalf("doctor did not print report header; got: %s", buf.String())
	}
}

func TestDoctorCmd_JSONFlag(t *testing.T) {
	cmd := doctorCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	tmp := t.TempDir()
	cmd.SetArgs([]string{"--config", filepath.Join(tmp, "config.toml"), "--json"})
	_ = cmd.Execute()
	out := buf.String()
	if !strings.Contains(out, `"results"`) {
		t.Fatalf("--json did not emit a results array; got: %s", out)
	}
}
