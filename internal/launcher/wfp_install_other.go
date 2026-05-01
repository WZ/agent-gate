//go:build !windows

package launcher

import "errors"

// InstallWFP is windows-only.
func InstallWFP() error { return errors.New("WFP install is windows-only") }

// UninstallWFP is windows-only.
func UninstallWFP() error { return errors.New("WFP uninstall is windows-only") }
