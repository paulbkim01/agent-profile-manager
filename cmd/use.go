package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/generate"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
)

var useGlobal bool

var useCmd = &cobra.Command{
	Use:   "use <profile>",
	Short: "Switch to a profile",
	Long:  "Per-shell by default. Use --global to set default for new shells.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return err
		}

		// Handle --unset
		if len(args) == 0 {
			unset, err := cmd.Flags().GetBool("unset")
			if err != nil {
				return fmt.Errorf("reading --unset flag: %w", err)
			}
			if unset {
				fmt.Println("unset APM_PROFILE")
				fmt.Println("unset CLAUDE_CONFIG_DIR")
				return nil
			}
			return fmt.Errorf("profile name required. Use 'apm use <profile>' or 'apm use --unset'")
		}

		name := args[0]

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

		// Detect if running inside eval or not
		fi, err := os.Stdout.Stat()
		if err != nil {
			return fmt.Errorf("checking stdout: %w", err)
		}
		isTTY := fi.Mode()&os.ModeCharDevice != 0

		if isTTY {
			// Not inside eval -- print instructions to stderr
			fmt.Fprintf(os.Stderr, "To activate in this shell, run:\n")
			fmt.Fprintf(os.Stderr, "  eval \"$(apm use %s)\"\n\n", name)
			fmt.Fprintf(os.Stderr, "Or add shell integration to your rc file:\n")
			fmt.Fprintf(os.Stderr, "  eval \"$(apm init bash)\"  # or zsh\n")

			if useGlobal {
				fmt.Fprintf(os.Stderr, "\nGlobal default set to '%s'.\n", name)
			}
		}

		// Always output the export statements (for eval to pick up)
		fmt.Printf("export APM_PROFILE=%s\n", name)
		fmt.Printf("export CLAUDE_CONFIG_DIR=%s\n", genDir)

		return nil
	},
}

func init() {
	useCmd.Flags().BoolVar(&useGlobal, "global", false, "set as default for all new shells")
	useCmd.Flags().Bool("unset", false, "deactivate profile in current shell")
	rootCmd.AddCommand(useCmd)
}
