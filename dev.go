//go:build dev

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
)

// devConfigDir returns a stable per-user temp directory for dev mode.
// Only the APM config (profiles, generated, common) is sandboxed.
// ClaudeDir still points to real ~/.claude/ so --from current works
// and generated profiles have proper shared-item symlinks.
func devConfigDir() string {
	dir := filepath.Join(os.TempDir(), "apm-dev-"+strconv.Itoa(os.Getuid()))
	os.MkdirAll(dir, 0o755)
	return dir
}

func init() {
	rootCmd.Short += " [dev]"
	rootCmd.Long += "\n\nDEV BUILD — using sandboxed config by default."

	origPreRun := rootCmd.PersistentPreRun
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Auto-sandbox: if --config-dir wasn't explicitly set, use dev temp dir.
		if !cmd.Flags().Changed("config-dir") {
			dir := devConfigDir()
			cmd.Flags().Set("config-dir", dir)
			fmt.Fprintf(os.Stderr, "apm [dev]: using sandbox %s\n", dir)
		}
		if origPreRun != nil {
			origPreRun(cmd, args)
		}
	}
}
