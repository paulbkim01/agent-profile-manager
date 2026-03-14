package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
)

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete a profile",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		name := args[0]
		log.Printf("delete: deleting profile '%s' (force=%v)", name, deleteForce)

		// Check if this profile was the global default before deletion
		wasDefault := false
		if dp, err := cfg.DefaultProfile(); err == nil && dp == name {
			wasDefault = true
		}

		if err := profile.Delete(cfg, name, deleteForce); err != nil {
			return fmt.Errorf("deleting profile: %w", err)
		}

		fmt.Printf("Deleted profile '%s'\n", name)

		// Inform user if the global default was cleared
		if wasDefault {
			fmt.Fprintf(os.Stderr, "Note: global default profile has been cleared.\n")
		}

		// Warn if it was active in this shell
		if os.Getenv("APM_PROFILE") == name {
			fmt.Fprintf(os.Stderr, "Warning: '%s' is still active in this shell. CLAUDE_CONFIG_DIR points to a removed directory.\n", name)
			fmt.Fprintf(os.Stderr, "Run immediately: eval \"$(apm use --unset)\"\n")
		}
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVar(&deleteForce, "force", false, "delete even if profile is active")
	rootCmd.AddCommand(deleteCmd)
}
