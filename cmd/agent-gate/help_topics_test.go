package main

import (
	"strings"
	"testing"
)

func TestHelpTopics_Registered(t *testing.T) {
	root := newRootCmd()
	want := map[string]bool{"allowlist": false, "denylist": false, "passthrough": false}
	for _, sub := range root.Commands() {
		if _, ok := want[sub.Use]; ok {
			want[sub.Use] = true
			if !strings.Contains(sub.Long, sub.Use) {
				t.Errorf("help topic %q has empty or unrelated Long help", sub.Use)
			}
		}
	}
	for use, found := range want {
		if !found {
			t.Errorf("help topic %q missing from root", use)
		}
	}
}

func TestRootCmd_GroupsAssigned(t *testing.T) {
	root := newRootCmd()
	wantGroups := map[string]string{
		"init":    "start",
		"doctor":  "start",
		"run":     "daily",
		"cert":    "maint",
		"version": "maint",
	}
	for _, sub := range root.Commands() {
		if want, ok := wantGroups[sub.Use]; ok && sub.GroupID != want {
			t.Errorf("command %q in group %q, want %q", sub.Use, sub.GroupID, want)
		}
	}
}
