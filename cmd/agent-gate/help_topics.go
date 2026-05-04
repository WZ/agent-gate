package main

import "github.com/spf13/cobra"

func helpTopicAllowlist() *cobra.Command {
	return &cobra.Command{
		Use:   "allowlist",
		Short: "Explain agent-gate's allowlist semantics",
		Long: `THE ALLOWLIST

agent-gate keeps a file at ~/.config/agent-gate/allowlist.txt listing hosts
that are considered "trusted." When a request hits a trusted host, the
host_not_allowlisted flag is suppressed — but the request is still
captured, parsed, and stored.

WHEN TO USE

Add a host to the allowlist when:
  - You've already validated the host belongs to your agent's expected
    upstreams (api.anthropic.com, api.openai.com, etc.)
  - You want the dashboard's flag count to focus on real anomalies
    rather than every-request-from-this-vendor noise.

ENFORCEMENT MODE

By default, the allowlist is annotation-only — non-allowlisted hosts pass
but get flagged. Pass --enforce-allowlist on agent-gate run to make the
proxy return 403 for any host not in the allowlist.

HOW TO MUTATE

  1. Dashboard "Trust this host" button (POST /api/trust)
  2. Dashboard "Untrust" button (POST /api/untrust)
  3. agent-gate init --allow-host HOST (init time only)
  4. $EDITOR ~/.config/agent-gate/allowlist.txt (manual)

FILE FORMAT

  - One hostname per line
  - Lines starting with # are comments
  - Lowercased on write; case-insensitive on match
  - Mode 0600
`,
	}
}

func helpTopicDenylist() *cobra.Command {
	return &cobra.Command{
		Use:   "denylist",
		Short: "Explain agent-gate's denylist semantics",
		Long: `THE DENYLIST

A denylisted host always loses. The proxy returns a synthetic 403 to the
agent without contacting the upstream. The flow is still recorded for
audit (you see the attempt, the host, the time, the agent's user-agent,
etc.) — but no bytes leave your machine for that request.

WHEN TO USE

Add to the denylist when:
  - You've decided your agent should never reach a domain (e.g., a
    competitor's API, a known data-exfil endpoint, a cloud metadata
    service like 169.254.169.254).
  - The denylist takes precedence over allowlist and passthrough.

HOW TO MUTATE

  1. Dashboard "Block this host" button (POST /api/block)
  2. $EDITOR ~/.config/agent-gate/denylist.txt
`,
	}
}

func helpTopicPassthrough() *cobra.Command {
	return &cobra.Command{
		Use:   "passthrough",
		Short: "Explain agent-gate's passthrough semantics",
		Long: `THE PASSTHROUGH LIST

Passthrough hosts are tunneled raw — agent-gate accepts the TCP connect,
opens a TCP connection to the upstream, and shuttles bytes back and
forth WITHOUT TLS interception. You see connection metadata (host, port,
timestamps, byte counts) but NOT the request or response payload.

WHEN TO USE

Use passthrough only when:
  - The upstream certificate-pins agent-gate's MITM CA away (the agent
    refuses to talk because the cert chain doesn't match what it expects).
  - A common case: mcp-proxy.anthropic.com on Claude Code's MCP path.

If you're not sure whether a host pins, leave it out — capture the
default MITM behavior and audit the body.

HOW TO MUTATE

  1. Dashboard "Passthrough (no MITM)" button (POST /api/passthrough)
  2. $EDITOR ~/.config/agent-gate/passthrough.txt

NOTE: passthrough changes only take effect on the NEXT agent-gate run;
the proxy reads the list once at startup.
`,
	}
}
