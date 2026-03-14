package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
)

var lsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List all profiles",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		profiles, err := profile.List(cfg)
		if err != nil {
			return fmt.Errorf("listing profiles: %w", err)
		}

		log.Printf("ls: found %d profiles", len(profiles))

		if len(profiles) == 0 {
			fmt.Println("No profiles. Create one with: apm create <name>")
			return nil
		}

		defaultProfile, err := cfg.DefaultProfile()
		if err != nil {
			return fmt.Errorf("reading default profile: %w", err)
		}
		shellProfile := os.Getenv("APM_PROFILE")

		for _, p := range profiles {
			marker := "  "
			status := ""

			if p.Meta.Name == shellProfile {
				marker = "* "
				status = " (this shell)"
			} else if p.Meta.Name == defaultProfile {
				marker = "* "
				status = " (global default)"
			}

			desc := p.Meta.Description
			if desc != "" {
				desc = " - " + desc
			}

			fmt.Printf("%s%s%s%s\n", marker, p.Meta.Name, desc, status)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(lsCmd)
}
