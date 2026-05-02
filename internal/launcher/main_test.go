package launcher

import (
	"fmt"
	"os"
	"testing"
)

// TestMain intercepts the `__netns-helper` hidden subcommand when the test
// binary is re-exec'd by airtightFeasible() / spawnAirtight() on Linux.
// Without this, os.Executable() returns the test binary, which would not
// dispatch the helper code path — the probe would always exit cleanly
// (returning feasible=true) and the real spawn would silently fail to
// send the listener FD, surfacing as "no SCM_RIGHTS in message".
func TestMain(m *testing.M) {
	if len(os.Args) >= 2 && os.Args[1] == "__netns-helper" {
		if err := RunNetnsHelper(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// On non-linux RunNetnsHelper returns the linux-only error; on linux,
		// success path syscall.Exec's the target and never returns here.
		return
	}
	os.Exit(m.Run())
}
