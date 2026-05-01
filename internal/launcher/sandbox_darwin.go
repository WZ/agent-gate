//go:build darwin

package launcher

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const sandboxExecPath = "/usr/bin/sandbox-exec"

// sandboxProfileTemplate is the SBPL profile applied to the target process.
// %d is replaced with the proxy port (1 occurrence).
// sandbox-exec only accepts "localhost" or "*" as host in remote ip rules;
// "127.0.0.1" is rejected. "localhost" covers both IPv4 and IPv6 loopback.
const sandboxProfileTemplate = `(version 1)
(allow default)

(deny network*)

(allow network*
  (remote ip "localhost:%d"))

(allow network-bind (local ip "localhost:*"))
`

func buildSandboxProfile(port int) string {
	return fmt.Sprintf(sandboxProfileTemplate, port)
}

// airtightFeasible checks that sandbox-exec exists. Almost always true on macOS.
func airtightFeasible() (bool, string) {
	if _, err := os.Stat(sandboxExecPath); err != nil {
		return false, fmt.Sprintf("%s missing: %v", sandboxExecPath, err)
	}
	return true, ""
}

func spawnAirtight(ctx context.Context, opts Options, env []string) (*childHandle, error) {
	port, err := portFromAddr(opts.ProxyAddr)
	if err != nil {
		return nil, err
	}
	profile := buildSandboxProfile(port)

	args := []string{"-p", profile, opts.Cmd}
	args = append(args, opts.Args...)
	cmd := exec.CommandContext(ctx, sandboxExecPath, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.Stdin, opts.Stdout, opts.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox-exec start: %w", err)
	}
	return &childHandle{cmd: cmd}, nil
}

func portFromAddr(addr string) (int, error) {
	if addr == "" {
		return 8888, nil
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0, errors.New("invalid port in ProxyAddr")
	}
	return port, nil
}

type childHandle struct {
	cmd *exec.Cmd
}

func (h *childHandle) wait(ctx context.Context) (int, error) {
	if h == nil || h.cmd == nil {
		return 1, errors.New("launcher: nil childHandle")
	}
	return waitCmd(ctx, h.cmd)
}

func (h *childHandle) kill() error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Kill()
}
