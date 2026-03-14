# Task 1: Remaining CLI commands

## Files to create
- `cmd/create.go`
- `cmd/delete.go`
- `cmd/edit.go`
- `cmd/ls.go`
- `cmd/describe.go`
- `cmd/regenerate.go`

## Dependencies
- Phase 5 (switching + shell integration)

## Implementation

### cmd/create.go

```go
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
		cfg.EnsureDirs()

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
```

### cmd/delete.go

```go
package cmd

import (
	"fmt"
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
```

### cmd/edit.go

```go
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
			return err
		}

		name := args[0]
		if !profile.Exists(cfg, name) {
			return fmt.Errorf("profile '%s' not found", name)
		}

		settingsPath := filepath.Join(cfg.ProfileDir(name), "settings.json")

		// Find editor
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = os.Getenv("EDITOR")
		}
		if editor == "" {
			editor = "vi"
		}

		// Open editor
		editorCmd := exec.Command(editor, settingsPath)
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

		// Regenerate if this profile is active
		active := os.Getenv("APM_PROFILE")
		if active == name || cfg.DefaultProfile() == name {
			if err := generate.Profile(cfg, name); err != nil {
				return fmt.Errorf("regenerating: %w", err)
			}
			fmt.Println("Profile regenerated.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(editCmd)
}
```

### cmd/ls.go

```go
package cmd

import (
	"fmt"
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
			return err
		}

		profiles, err := profile.List(cfg)
		if err != nil {
			return err
		}

		if len(profiles) == 0 {
			fmt.Println("No profiles. Create one with: apm create <name>")
			return nil
		}

		defaultProfile := cfg.DefaultProfile()
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
```

### cmd/describe.go

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
			fmt.Printf("\n%s: %v\n", dir, names)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(describeCmd)
}
```

### cmd/regenerate.go

```go
package cmd

import (
	"fmt"

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
			return fmt.Errorf("profile name required, or use --all")
		}

		name := args[0]
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
```

## Verification

```bash
go build -o apm .
./apm create personal --from current --description "personal projects"
./apm create work --from current --description "EAK work"
./apm ls
./apm describe work
./apm edit work
./apm regenerate --all
./apm delete personal
```
