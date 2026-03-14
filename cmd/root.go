package cmd

import (
	"io"
	"log"
	"os"

	"github.com/spf13/cobra"
)

// Package-level flag vars — if you add a new one, also add it to resetFlags() in cmd_test.go.
var (
	debug     bool
	configDir string
)

var rootCmd = &cobra.Command{
	Use:   "apm",
	Short: "Agent Profile Manager - switch Claude Code profiles",
	Long:  "Manage multiple Claude Code profiles with per-shell or global switching.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if debug {
			log.SetOutput(os.Stderr)
			log.SetFlags(log.Ltime | log.Lshortfile)
			log.Println("debug mode enabled")
		} else {
			log.SetOutput(io.Discard)
		}
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "verbose logging to stderr")
	rootCmd.PersistentFlags().StringVar(&configDir, "config-dir", "", "override config directory (default: ~/.config/apm)")
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
}
