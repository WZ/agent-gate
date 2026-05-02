//go:build windows

package launcher

import (
	"fmt"

	"github.com/tailscale/wf"
)

// InstallWFP registers the agent-gate WFP provider + sublayer.
// Idempotent: re-running is safe (already-existing entries are skipped).
//
// Must run elevated (UAC). Modifying WFP requires admin permissions.
//
// KNOWN LIMITATION (v0.3.0): we do not yet grant the current user a per-sublayer
// DACL allowing non-elevated filter add/remove at runtime. Per the Plan 3
// design (§4.3), that's the long-term Windows runtime UX; the current
// implementation requires every `agent-gate run` to also be elevated, until a
// follow-up plan adds raw FwpmSubLayerSetSecurityInfoByKey0 + DACL plumbing.
func InstallWFP() error {
	sess, err := wf.New(&wf.Options{
		Name:        "agent-gate-install",
		Description: "agent-gate WFP install session",
	})
	if err != nil {
		return fmt.Errorf("wfp open: %w", err)
	}
	defer sess.Close()

	if err := ensureProvider(sess); err != nil {
		return err
	}
	if err := ensureSublayer(sess); err != nil {
		return err
	}
	return nil
}

func ensureProvider(sess *wf.Session) error {
	providers, err := sess.Providers()
	if err != nil {
		return fmt.Errorf("list providers: %w", err)
	}
	for _, p := range providers {
		if p.ID == agentGateProviderID {
			return nil
		}
	}
	if err := sess.AddProvider(&wf.Provider{
		ID:          agentGateProviderID,
		Name:        "agent-gate",
		Description: "agent-gate audit gate",
		Persistent:  true,
	}); err != nil {
		return fmt.Errorf("AddProvider: %w", err)
	}
	return nil
}

func ensureSublayer(sess *wf.Session) error {
	sublayers, err := sess.Sublayers(agentGateProviderID)
	if err != nil {
		return fmt.Errorf("list sublayers: %w", err)
	}
	for _, sl := range sublayers {
		if sl.ID == agentGateSubLayerID {
			return nil
		}
	}
	if err := sess.AddSublayer(&wf.Sublayer{
		ID:          agentGateSubLayerID,
		Name:        "agent-gate-default",
		Description: "agent-gate per-run filters",
		Provider:    agentGateProviderID,
		Persistent:  true,
		Weight:      0x100,
	}); err != nil {
		return fmt.Errorf("AddSublayer: %w", err)
	}
	return nil
}

// UninstallWFP removes our sublayer + provider. Must run elevated.
// Idempotent: missing entries are not an error.
func UninstallWFP() error {
	sess, err := wf.New(&wf.Options{Name: "agent-gate-uninstall"})
	if err != nil {
		return fmt.Errorf("wfp open: %w", err)
	}
	defer sess.Close()
	// Errors here are best-effort: tailscale/wf does not expose a not-found
	// sentinel, and a missing entry is the desired post-state of uninstall.
	_ = sess.DeleteSublayer(agentGateSubLayerID)
	_ = sess.DeleteProvider(agentGateProviderID)
	return nil
}
