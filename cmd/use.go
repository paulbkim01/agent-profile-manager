package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/generate"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
	"github.com/paulbkim/agent-profile-manager/internal/validate"
)

// shellQuote escapes a string for safe use inside POSIX single quotes.
// The standard technique: replace each ' with '\'' (end quote, literal ', start quote).
func shellQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// isStdoutTTY reports whether stdout is a terminal.
// Returns false if stdout is a pipe, file, or if the stat fails.
func isStdoutTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Flag var — also listed in resetFlags() in cmd_test.go.
var useGlobal bool

var useCmd = &cobra.Command{
	Use:   "use <profile>",
	Short: "Switch to a profile",
	Long:  "Per-shell by default. Use --global to set default for new shells.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Detect if stdout is a terminal (not inside eval/pipe)
		isTTY := isStdoutTTY()

		// Handle --unset
		if len(args) == 0 {
			unset, err := cmd.Flags().GetBool("unset")
			if err != nil {
				return fmt.Errorf("reading --unset flag: %w", err)
			}
			if unset {
				if isTTY {
					// Direct TTY: show guidance, no shell code on stdout
					fmt.Fprintf(os.Stderr, "To deactivate in this shell, run:\n")
					fmt.Fprintf(os.Stderr, "  eval \"$(apm use --unset)\"\n")
				} else {
					// Inside eval/pipe: emit shell code
					fmt.Println("unset APM_PROFILE")
					fmt.Println("unset CLAUDE_CONFIG_DIR")
				}
				return nil
			}
			return fmt.Errorf("profile name required. Use 'apm use <profile>' or 'apm use --unset'")
		}

		name := args[0]
		if err := validate.ProfileName(name); err != nil {
			return fmt.Errorf("invalid profile name: %w", err)
		}
		log.Printf("use: switching to profile '%s' (global=%v)", name, useGlobal)

		// Check profile exists
		if !profile.Exists(cfg, name) {
			return fmt.Errorf("profile '%s' not found. Run 'apm ls' to see available profiles", name)
		}

		// Regenerate
		if err := generate.Profile(cfg, name); err != nil {
			return fmt.Errorf("generating profile: %w", err)
		}

		genDir := cfg.GeneratedProfileDir(name)

		// Global: also update config.yaml
		if useGlobal {
			if err := cfg.SetDefaultProfile(name); err != nil {
				return fmt.Errorf("setting default: %w", err)
			}
			log.Printf("use: set global default to '%s'", name)
		}

		if isTTY {
			// Direct TTY invocation: no shell code on stdout, only guidance on stderr
			if useGlobal {
				fmt.Fprintf(os.Stderr, "Global default set to '%s'.\n", name)
				fmt.Fprintf(os.Stderr, "New shells will auto-activate this profile.\n")
			} else {
				fmt.Fprintf(os.Stderr, "To activate in this shell, run:\n")
				fmt.Fprintf(os.Stderr, "  eval \"$(apm use %s)\"\n\n", name)
				fmt.Fprintf(os.Stderr, "Or add shell integration to your rc file:\n")
				fmt.Fprintf(os.Stderr, "  eval \"$(apm init bash)\"  # or zsh\n")
			}
		} else {
			// Inside eval/pipe: emit shell-safe exports
			fmt.Printf("export APM_PROFILE='%s'\n", shellQuote(name))
			fmt.Printf("export CLAUDE_CONFIG_DIR='%s'\n", shellQuote(genDir))
		}

		return nil
	},
}

func init() {
	useCmd.Flags().BoolVar(&useGlobal, "global", false, "set as default for all new shells")
	useCmd.Flags().Bool("unset", false, "deactivate profile in current shell")
	rootCmd.AddCommand(useCmd)
}
