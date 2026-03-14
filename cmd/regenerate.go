package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/generate"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
)

var regenAll bool

var regenerateCmd = &cobra.Command{
	Use:     "regenerate [name]",
	Aliases: []string{"regen"},
	Short:   "Rebuild generated profile directories",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return err
		}

		if regenAll {
			log.Printf("regenerate: regenerating all profiles")
			profiles, err := profile.List(cfg)
			if err != nil {
				return err
			}
			for _, p := range profiles {
				if err := generate.Profile(cfg, p.Meta.Name); err != nil {
					return fmt.Errorf("regenerating '%s': %w", p.Meta.Name, err)
				}
				fmt.Printf("Regenerated '%s'\n", p.Meta.Name)
			}
			return nil
		}

		if len(args) == 0 {
			return fmt.Errorf("profile name required. Use 'apm regenerate <name>' or --all")
		}

		name := args[0]
		log.Printf("regenerate: regenerating profile '%s'", name)
		if err := generate.Profile(cfg, name); err != nil {
			return err
		}
		fmt.Printf("Regenerated '%s'\n", name)
		return nil
	},
}

func init() {
	regenerateCmd.Flags().BoolVar(&regenAll, "all", false, "regenerate all profiles")
	rootCmd.AddCommand(regenerateCmd)
}
