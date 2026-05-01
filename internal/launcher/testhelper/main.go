// Command testhelper is a small target used by launcher tests. It supports:
//
//	testhelper -dial host:port -timeout 2s         (uses default Go net.Dial; proxy env honored)
//	testhelper -dial-direct ip:port -timeout 2s    (low-level socket; ignores HTTPS_PROXY)
//	testhelper -udp host:port                      (UDP write; for DNS-style tests)
//	testhelper -spawn cmd args...                  (spawns a subprocess and waits)
//	testhelper -exit N                             (immediately exits with code N)
//
// Exit codes:
//
//	0  — success
//	1  — generic failure
//	2  — bad arguments
//	42 — used by exit-code-propagation test
package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func main() {
	var (
		dial       = flag.String("dial", "", "host:port (uses net.Dial; honors proxy env)")
		dialDirect = flag.String("dial-direct", "", "ip:port (uses syscall socket; bypasses proxy env)")
		udp        = flag.String("udp", "", "host:port for a UDP write")
		spawn      = flag.String("spawn", "", "subprocess to spawn (rest of os.Args after -- are passed)")
		timeout    = flag.Duration("timeout", 2*time.Second, "operation timeout")
		exitCode   = flag.Int("exit", -1, "if >=0, exit with this code immediately")
	)
	flag.Parse()

	if *exitCode >= 0 {
		os.Exit(*exitCode)
	}

	switch {
	case *dial != "":
		os.Exit(doDial(*dial, *timeout))
	case *dialDirect != "":
		os.Exit(doDialDirect(*dialDirect, *timeout))
	case *udp != "":
		os.Exit(doUDP(*udp, *timeout))
	case *spawn != "":
		os.Exit(doSpawn(*spawn, flag.Args(), *timeout))
	default:
		fmt.Fprintln(os.Stderr, "testhelper: provide -dial / -dial-direct / -udp / -spawn / -exit")
		os.Exit(2)
	}
}

func doDial(addr string, timeout time.Duration) int {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial %s: %v\n", addr, err)
		return 1
	}
	conn.Close()
	return 0
}

// doDialDirect intentionally does not honor HTTPS_PROXY/NO_PROXY env vars.
// We use the standard net.Dialer which speaks raw TCP; HTTP-level proxy honoring
// happens above the transport, so this is already proxy-bypassing.
func doDialDirect(addr string, timeout time.Duration) int {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "split %s: %v\n", addr, err)
		return 2
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		fmt.Fprintf(os.Stderr, "bad port %q\n", portStr)
		return 2
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Resolve manually to avoid OS-level proxy DNS forwarding.
		ips, err := net.LookupIP(host)
		if err != nil || len(ips) == 0 {
			fmt.Fprintf(os.Stderr, "lookup %s: %v\n", host, err)
			return 1
		}
		ip = ips[0]
	}
	d := &net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial-direct %s: %v\n", addr, err)
		return 1
	}
	conn.Close()
	return 0
}

func doUDP(addr string, timeout time.Duration) int {
	conn, err := net.DialTimeout("udp", addr, timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "udp %s: %v\n", addr, err)
		return 1
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte("ping")); err != nil {
		fmt.Fprintf(os.Stderr, "udp write: %v\n", err)
		return 1
	}
	return 0
}

func doSpawn(prog string, args []string, timeout time.Duration) int {
	cmd := exec.Command(prog, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start: %v\n", err)
		return 1
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				return exitErr.ExitCode()
			}
			return 1
		}
		return 0
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		fmt.Fprintf(os.Stderr, "spawn timeout after %v\n", timeout)
		return 1
	}
}
