package cmd

import (
	"fmt"

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
			return err
		}
		if err := cfg.EnsureDirs(); err != nil {
			return fmt.Errorf("ensuring directories: %w", err)
		}

		name := args[0]
		if err := profile.Create(cfg, name, createFrom, createDesc); err != nil {
			return err
		}

		fmt.Printf("Created profile '%s'\n", name)
		if createFrom == "" {
			fmt.Printf("Edit it with: apm edit %s\n", name)
		}
		fmt.Printf("Activate with: eval \"$(apm use %s)\"\n", name)
		return nil
	},
}

func init() {
	createCmd.Flags().StringVar(&createFrom, "from", "", "import from 'current' or another profile name")
	createCmd.Flags().StringVar(&createDesc, "description", "", "profile description")
	rootCmd.AddCommand(createCmd)
}
