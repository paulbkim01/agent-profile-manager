package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
)

var describeCmd = &cobra.Command{
	Use:   "describe <name>",
	Short: "Show profile details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return err
		}

		name := args[0]
		log.Printf("describe: loading profile '%s'", name)
		info, err := profile.Get(cfg, name)
		if err != nil {
			return err
		}

		fmt.Printf("Profile: %s\n", info.Meta.Name)
		if info.Meta.Description != "" {
			fmt.Printf("Description: %s\n", info.Meta.Description)
		}
		fmt.Printf("Created: %s\n", info.Meta.CreatedAt)
		if info.Meta.Source != "" {
			fmt.Printf("Source: %s\n", info.Meta.Source)
		}
		fmt.Printf("Dir: %s\n", info.Dir)

		// Show settings overrides
		settingsPath := filepath.Join(info.Dir, "settings.json")
		if data, err := os.ReadFile(settingsPath); err == nil {
			var settings map[string]any
			if json.Unmarshal(data, &settings) == nil && len(settings) > 0 {
				fmt.Printf("\nSettings overrides:\n")
				for key, val := range settings {
					switch v := val.(type) {
					case map[string]any:
						fmt.Printf("  %s: {%d keys}\n", key, len(v))
					case []any:
						fmt.Printf("  %s: [%d items]\n", key, len(v))
					default:
						fmt.Printf("  %s: %v\n", key, v)
					}
				}
			}
		}

		// Show managed dirs content
		for _, dir := range []string{"skills", "commands", "agents"} {
			dirPath := filepath.Join(info.Dir, dir)
			entries, err := os.ReadDir(dirPath)
			if err != nil || len(entries) == 0 {
				continue
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			fmt.Printf("\n%s: %s\n", dir, strings.Join(names, ", "))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(describeCmd)
}
