package main

import (
	"fmt"
	"os"

	"github.com/paulbkim/agent-profile-manager/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// "apm current" with no active profile should exit 1 silently,
		// so statusline scripts don't see "error: no active profile".
		if cmd.IsNoActiveProfile(err) {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
