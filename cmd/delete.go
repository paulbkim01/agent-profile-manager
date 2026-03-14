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
			return err
		}

		name := args[0]
		log.Printf("delete: deleting profile '%s' (force=%v)", name, deleteForce)
		if err := profile.Delete(cfg, name, deleteForce); err != nil {
			return err
		}

		fmt.Printf("Deleted profile '%s'\n", name)

		// Warn if it was active in this shell
		if os.Getenv("APM_PROFILE") == name {
			fmt.Fprintf(os.Stderr, "Warning: '%s' was active in this shell.\n", name)
			fmt.Fprintf(os.Stderr, "Run: eval \"$(apm use --unset)\"\n")
		}
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVar(&deleteForce, "force", false, "delete even if profile is active")
	rootCmd.AddCommand(deleteCmd)
}
