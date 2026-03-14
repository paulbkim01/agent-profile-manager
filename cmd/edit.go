package cmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/generate"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
	"github.com/paulbkim/agent-profile-manager/internal/validate"
)

var editCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit a profile's settings.json in $EDITOR",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		name := args[0]
		if !profile.Exists(cfg, name) {
			return fmt.Errorf("profile '%s' not found. Run 'apm ls' to see available profiles", name)
		}

		settingsPath := filepath.Join(cfg.ProfileDir(name), "settings.json")

		// Find editor: VISUAL > EDITOR > vi
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}

		log.Printf("edit: opening '%s' with editor '%s'", settingsPath, editor)

		// Open editor — split editor string so multi-word values like
		// "code --wait" work correctly.
		parts := strings.Fields(editor)
		editorCmd := exec.Command(parts[0], append(parts[1:], settingsPath)...)
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		if err := editorCmd.Run(); err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}

		// After editor exits, validate
		if err := validate.SettingsJSON(settingsPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			fmt.Fprintf(os.Stderr, "Run 'apm edit %s' to fix it.\n", name)
			return nil // don't regenerate broken settings
		}

		// Regenerate if this profile is active (in shell or as global default)
		active := os.Getenv("APM_PROFILE")
		defaultProfile, err := cfg.DefaultProfile()
		if err != nil {
			return fmt.Errorf("reading default profile: %w", err)
		}
		if active == name || defaultProfile == name {
			if err := generate.Profile(cfg, name); err != nil {
				return fmt.Errorf("regenerating profile: %w", err)
			}
			fmt.Println("Profile regenerated.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(editCmd)
}
