package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
)

// errNoActiveProfile is a sentinel error for "no profile active".
// We use a specific type so callers (like statusline scripts) can rely on
// exit code 1 without Cobra printing usage or error text.
type errNoActiveProfile struct{}

func (errNoActiveProfile) Error() string { return "no active profile" }

var currentCmd = &cobra.Command{
	Use:   "current",
	Short: "Output the active profile name",
	Long:  "For statusline scripts. Outputs just the name, nothing else.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check env var first (per-shell override)
		if name := os.Getenv("APM_PROFILE"); name != "" {
			fmt.Println(name)
			return nil
		}

		// Fall back to config.yaml default
		cfg, err := config.Load(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		name, err := cfg.DefaultProfile()
		if err != nil {
			return fmt.Errorf("reading default profile: %w", err)
		}

		if name != "" {
			fmt.Println(name)
			return nil
		}

		// No profile active — return sentinel error (exit code 1, no noise)
		return errNoActiveProfile{}
	},
}

func init() {
	rootCmd.AddCommand(currentCmd)
}
