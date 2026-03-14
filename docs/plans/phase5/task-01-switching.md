# Task 1: Use command + shell integration

## Files to create
- `cmd/use.go`
- `cmd/current.go`
- `cmd/init_shell.go`
- `internal/shell/shell.go`

## Dependencies
- Phase 4 (generation engine)

## Implementation

### cmd/use.go

The `use` command has two modes:
- Per-shell (default): outputs `export` statements for eval
- Global (`--global`): writes default to config.yaml and also outputs exports

```go
package cmd

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/generate"
	"github.com/paulbkim/agent-profile-manager/internal/profile"
)

var useGlobal bool

var useCmd = &cobra.Command{
	Use:   "use <profile>",
	Short: "Switch to a profile",
	Long:  "Per-shell by default. Use --global to set default for new shells.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configDir)
		if err != nil {
			return err
		}

		// Handle --unset
		if len(args) == 0 {
			unset, _ := cmd.Flags().GetBool("unset")
			if unset {
				fmt.Println("unset APM_PROFILE")
				fmt.Println("unset CLAUDE_CONFIG_DIR")
				return nil
			}
			return fmt.Errorf("profile name required. Use 'apm use <profile>' or 'apm use --unset'")
		}

		name := args[0]

		// Check profile exists
		if !profile.Exists(cfg, name) {
			return fmt.Errorf("profile '%s' not found. Run 'apm ls' to see available profiles", name)
		}

		// Regenerate
		if err := generate.Profile(cfg, name); err != nil {
			return fmt.Errorf("generating profile: %w", err)
		}

		genDir := cfg.GeneratedProfileDir(name)

		// Global: also update config.yaml
		if useGlobal {
			if err := cfg.SetDefaultProfile(name); err != nil {
				return fmt.Errorf("setting default: %w", err)
			}
			log.Printf("use: set global default to '%s'", name)
		}

		// Detect if running inside eval or not
		fi, _ := os.Stdout.Stat()
		isTTY := fi.Mode()&os.ModeCharDevice != 0

		if isTTY {
			// Not inside eval -- print instructions
			fmt.Fprintf(os.Stderr, "To activate in this shell, run:\n")
			fmt.Fprintf(os.Stderr, "  eval \"$(apm use %s)\"\n\n", name)
			fmt.Fprintf(os.Stderr, "Or add shell integration to your rc file:\n")
			fmt.Fprintf(os.Stderr, "  eval \"$(apm init bash)\"  # or zsh\n")

			if useGlobal {
				fmt.Fprintf(os.Stderr, "\nGlobal default set to '%s'.\n", name)
			}
		}

		// Always output the export statements (for eval to pick up)
		fmt.Printf("export APM_PROFILE=%s\n", name)
		fmt.Printf("export CLAUDE_CONFIG_DIR=%s\n", genDir)

		return nil
	},
}

func init() {
	useCmd.Flags().BoolVar(&useGlobal, "global", false, "set as default for all new shells")
	useCmd.Flags().Bool("unset", false, "deactivate profile in current shell")
	rootCmd.AddCommand(useCmd)
}
```

### cmd/current.go

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/config"
)

var currentCmd = &cobra.Command{
	Use:   "current",
	Short: "Output the active profile name",
	Long:  "For statusline scripts. Outputs just the name, nothing else.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check env var first (per-shell)
		if name := os.Getenv("APM_PROFILE"); name != "" {
			fmt.Println(name)
			return nil
		}

		// Fall back to config.yaml default
		cfg, err := config.Load(configDir)
		if err != nil {
			os.Exit(1)
			return nil
		}

		if name := cfg.DefaultProfile(); name != "" {
			fmt.Println(name)
			return nil
		}

		// No profile active
		os.Exit(1)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(currentCmd)
}
```

### cmd/init_shell.go

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/shell"
)

var initCmd = &cobra.Command{
	Use:   "init <bash|zsh>",
	Short: "Output shell integration code",
	Long:  "Add 'eval \"$(apm init bash)\"' to your .bashrc or .zshrc",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		shellType := args[0]
		script, err := shell.InitScript(shellType)
		if err != nil {
			return err
		}
		fmt.Print(script)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
```

### internal/shell/shell.go

```go
package shell

import "fmt"

const bashInit = `
# Agent Profile Manager shell integration
apm() {
  case "$1" in
    use)
      local output
      output=$(command apm "$@" 2>/dev/null)
      local rc=$?
      if [ $rc -eq 0 ]; then
        eval "$output"
      else
        command apm "$@"
      fi
      ;;
    *)
      command apm "$@"
      ;;
  esac
}

# Auto-activate global default on shell startup
_apm_auto_activate() {
  if [ -z "$APM_PROFILE" ]; then
    local default_profile
    default_profile=$(command apm current 2>/dev/null)
    if [ -n "$default_profile" ]; then
      eval "$(command apm use "$default_profile" 2>/dev/null)"
    fi
  fi
}
_apm_auto_activate
`

const zshInit = `
# Agent Profile Manager shell integration
apm() {
  case "$1" in
    use)
      local output
      output=$(command apm "$@" 2>/dev/null)
      local rc=$?
      if [ $rc -eq 0 ]; then
        eval "$output"
      else
        command apm "$@"
      fi
      ;;
    *)
      command apm "$@"
      ;;
  esac
}

# Auto-activate global default on shell startup
_apm_auto_activate() {
  if [[ -z "$APM_PROFILE" ]]; then
    local default_profile
    default_profile=$(command apm current 2>/dev/null)
    if [[ -n "$default_profile" ]]; then
      eval "$(command apm use "$default_profile" 2>/dev/null)"
    fi
  fi
}
_apm_auto_activate
`

// InitScript returns the shell integration script for the given shell.
func InitScript(shellType string) (string, error) {
	switch shellType {
	case "bash":
		return bashInit, nil
	case "zsh":
		return zshInit, nil
	default:
		return "", fmt.Errorf("unsupported shell: %s (use 'bash' or 'zsh')", shellType)
	}
}
```

## Key design decisions

- The `apm` shell function intercepts `use` and evals its output. All other subcommands pass through.
- `_apm_auto_activate` runs once on shell startup. If `APM_PROFILE` is already set (e.g. inherited from parent shell), it skips activation.
- `apm current` exits with code 1 when no profile is active. This makes `$(apm current 2>/dev/null)` return empty string cleanly.
- The TTY detection in `use.go` prints helpful instructions when someone runs `apm use work` directly without eval.

## Verification

```bash
go build -o apm .
./apm init bash     # should output shell function
./apm current       # should exit 1 (no profile active)
./apm use work      # should detect TTY and print instructions
eval "$(./apm use work)"  # should set CLAUDE_CONFIG_DIR
echo $APM_PROFILE   # should print "work"
```
