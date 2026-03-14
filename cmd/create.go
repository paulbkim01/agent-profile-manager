package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
)

var (
	createFrom string
	createDesc string
)

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.EnsureDirs(); err != nil {
			return fmt.Errorf("ensuring directories: %w", err)
		}

		name := args[0]
		log.Printf("create: creating profile '%s' (from=%q, desc=%q)", name, createFrom, createDesc)
		if err := profile.Create(cfg, name, createFrom, createDesc); err != nil {
			return fmt.Errorf("creating profile: %w", err)
		}

		fmt.Printf("Created profile '%s'\n", name)
		if createFrom == "" {
			fmt.Fprintf(os.Stderr, "Edit it with: apm edit %s\n", name)
		}
		fmt.Fprintf(os.Stderr, "Activate with: eval \"$(apm use %s)\"\n", name)
		return nil
	},
}

func init() {
	createCmd.Flags().StringVar(&createFrom, "from", "", "import from 'current' or another profile name")
	createCmd.Flags().StringVar(&createDesc, "description", "", "profile description")
	rootCmd.AddCommand(createCmd)
}
