package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// ANSI color helpers for stderr messages.
var (
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorReset  = "\033[0m"
)

func logSuccess(format string, a ...any) {
	fmt.Fprintf(os.Stderr, colorGreen+"  ✓ "+colorReset+format+"\n", a...)
}

func logInfo(format string, a ...any) {
	fmt.Fprintf(os.Stderr, colorCyan+"  → "+colorReset+format+"\n", a...)
}

func logWarn(format string, a ...any) {
	fmt.Fprintf(os.Stderr, colorYellow+"  ⚠ "+colorReset+format+"\n", a...)
}

// Package-level flag vars — if you add a new one, also add it to resetFlags() in cli_commands_test.go.
var (
	debug     bool
	configDir string
)

var rootCmd = &cobra.Command{
	Use:   "apm",
	Short: "Agent Profile Manager - switch Claude Code profiles",
	Long:  "Manage multiple Claude Code profiles with per-shell or global switching.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if debug {
			log.SetOutput(os.Stderr)
			log.SetFlags(log.Ltime | log.Lshortfile)
			log.Println("debug mode enabled")
		} else {
			log.SetOutput(io.Discard)
		}
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Flag vars — also listed in resetFlags() in cli_commands_test.go.
var (
	createFrom    string
	createCurrent bool
	createDesc    string
)

// confirmOverwrite prompts the user to confirm overwriting an existing profile.
// Returns false if stdin is not interactive or user declines.
var confirmOverwrite = func(name string) bool {
	fmt.Fprintf(os.Stderr, "Profile '%s' already exists. Overwrite? [y/N]: ", name)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

// exactlyOneArg returns a cobra Args validator that requires exactly one argument
// with a custom error message when none is provided.
func exactlyOneArg(errMsg string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New(errMsg)
		}
		return cobra.ExactArgs(1)(cmd, args)
	}
}

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new profile (defaults to 'default')",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.EnsureDirs(); err != nil {
			return fmt.Errorf("ensuring directories: %w", err)
		}

		name := "default"
		if len(args) > 0 {
			name = args[0]
		}

		source := createFrom
		if createCurrent {
			source = "current"
		}

		log.Printf("create: creating profile '%s' (source=%q, desc=%q)", name, source, createDesc)

		// If profile already exists, prompt for overwrite
		if profileExists(cfg, name) {
			if !confirmOverwrite(name) {
				fmt.Fprintf(os.Stderr, "Aborted.\n")
				return nil
			}
			// Deactivate if this was the active profile
			activeProfile, _ := cfg.ActiveProfile()
			if activeProfile == name {
				if err := deactivateProfile(cfg); err != nil {
					return fmt.Errorf("deactivating profile: %w", err)
				}
			}
			if err := deleteProfile(cfg, name, true); err != nil {
				return fmt.Errorf("removing existing profile: %w", err)
			}
		}

		if err := createProfile(cfg, name, source, createDesc); err != nil {
			return fmt.Errorf("creating profile: %w", err)
		}

		fmt.Printf("Created profile '%s'\n", name)

		// Auto-activate the default profile
		if name == "default" {
			if err := cfg.SetDefaultProfile(name); err != nil {
				return fmt.Errorf("setting default: %w", err)
			}

			devMode := cmd.Flags().Changed("config-dir")
			activateErr := activateProfile(cfg, name, devMode)
			if errors.Is(activateErr, errSkipSymlink) {
				if err := generateProfile(cfg, name); err != nil {
					return fmt.Errorf("generating profile: %w", err)
				}
			} else if activateErr != nil {
				return fmt.Errorf("activating profile: %w", activateErr)
			}
		} else {
			if source == "" {
				fmt.Fprintf(os.Stderr, "Edit it with: apm edit %s\n", name)
			}
			fmt.Fprintf(os.Stderr, "Activate with: eval \"$(apm use %s)\"\n", name)
		}
		return nil
	},
}

// shellQuote escapes a string for safe use inside POSIX single quotes.
// The standard technique: replace each ' with '\'' (end quote, literal ', start quote).
func shellQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// isStdoutTTY reports whether stdout is a terminal.
// Returns false if stdout is a pipe, file, or if the stat fails.
func isStdoutTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Flag var — also listed in resetFlags() in cli_commands_test.go.
var useGlobal bool

var useCmd = &cobra.Command{
	Use:   "use <profile>",
	Short: "Switch to a profile",
	Long:  "Per-shell by default. Use --global to set default for new shells.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		isTTY := isStdoutTTY()
		devMode := cmd.Flags().Changed("config-dir")

		// Handle --unset
		if len(args) == 0 {
			unset, err := cmd.Flags().GetBool("unset")
			if err != nil {
				return fmt.Errorf("reading --unset flag: %w", err)
			}
			if unset {
				if devMode {
					// Dev mode: env-var-only deactivation
					if isTTY {
						fmt.Fprintf(os.Stderr, "To deactivate in this shell, run:\n")
						fmt.Fprintf(os.Stderr, "  eval \"$(apm use --unset)\"\n")
					} else {
						fmt.Println("unset APM_PROFILE")
						fmt.Println("unset CLAUDE_CONFIG_DIR")
					}
				} else {
					// Normal mode: remove symlink and restore backup
					if err := deactivateProfile(cfg); err != nil {
						return fmt.Errorf("deactivating: %w", err)
					}
					logSuccess("Profile deactivated. Restored ~/.claude from backup.")
					if !isTTY {
						fmt.Println("unset APM_PROFILE")
						fmt.Println("unset CLAUDE_CONFIG_DIR")
					}
				}
				return nil
			}
			return fmt.Errorf("profile name required. Use 'apm use <profile>' or 'apm use --unset'")
		}

		name := args[0]
		if err := validateProfileName(name); err != nil {
			return fmt.Errorf("invalid profile name: %w", err)
		}
		log.Printf("use: switching to profile '%s' (global=%v)", name, useGlobal)

		// Check profile exists
		if !profileExists(cfg, name) {
			return fmt.Errorf("profile '%s' not found. Run 'apm ls' to see available profiles", name)
		}

		// Global: also update config.yaml
		if useGlobal {
			if err := cfg.SetDefaultProfile(name); err != nil {
				return fmt.Errorf("setting default: %w", err)
			}
			log.Printf("use: set global default to '%s'", name)
		}

		// Try symlink activation (skipped in dev mode)
		err = activateProfile(cfg, name, devMode)
		if errors.Is(err, errSkipSymlink) {
			// Dev mode fallback: generate and output env var exports
			if err := generateProfile(cfg, name); err != nil {
				return fmt.Errorf("generating profile: %w", err)
			}
			genDir := cfg.GeneratedProfileDir(name)

			if isTTY {
				if useGlobal {
					fmt.Fprintf(os.Stderr, "Global default set to '%s'.\n", name)
					fmt.Fprintf(os.Stderr, "New shells will auto-activate this profile.\n")
				} else {
					fmt.Fprintf(os.Stderr, "To activate in this shell, run:\n")
					fmt.Fprintf(os.Stderr, "  eval \"$(apm use %s)\"\n\n", name)
					fmt.Fprintf(os.Stderr, "Or add shell integration to your rc file:\n")
					fmt.Fprintf(os.Stderr, "  eval \"$(apm init bash)\"  # or zsh\n")
				}
			} else {
				fmt.Printf("export APM_PROFILE='%s'\n", shellQuote(name))
				fmt.Printf("export CLAUDE_CONFIG_DIR='%s'\n", shellQuote(genDir))
			}
		} else if err != nil {
			return fmt.Errorf("activating profile: %w", err)
		} else {
			// Symlink succeeded
			logSuccess("Switched to '%s'", name)
			if useGlobal {
				logSuccess("Global default set to '%s'", name)
			}
			if !isTTY {
				fmt.Printf("export APM_PROFILE='%s'\n", shellQuote(name))
			}
		}

		return nil
	},
}

// Flag var — also listed in resetFlags() in cli_commands_test.go.
var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete a profile",
	Args:    exactlyOneArg("profile name required. Usage: apm delete <name>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		name := args[0]
		if err := validateProfileName(name); err != nil {
			return fmt.Errorf("invalid profile name: %w", err)
		}
		log.Printf("delete: deleting profile '%s' (force=%v)", name, deleteForce)

		// Read config once for both checks
		cf, err := cfg.readConfigFile()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		wasDefault := cf.DefaultProfile == name
		activeProfile := cf.ActiveProfile

		if err := deleteProfile(cfg, name, deleteForce); err != nil {
			return fmt.Errorf("deleting profile: %w", err)
		}

		logSuccess("Deleted profile '%s'", name)

		// Deactivate if we deleted the active profile, or if no profiles remain
		needsDeactivate := activeProfile == name
		if !needsDeactivate && activeProfile != "" {
			remaining, _ := listProfiles(cfg)
			if len(remaining) == 0 {
				needsDeactivate = true
			}
		}
		if needsDeactivate {
			logInfo("Removing symlink ~/.claude ...")
			logInfo("Cleaning generated directories ...")
			if err := deactivateProfile(cfg); err != nil {
				return fmt.Errorf("deactivating profile: %w", err)
			}
			logSuccess("Restored ~/.claude from backup")
		}

		if wasDefault {
			logWarn("Global default profile has been cleared.")
		}

		if os.Getenv("APM_PROFILE") == name {
			logWarn("'%s' is still set in this shell.", name)
			logInfo("Run: eval \"$(apm use --unset)\"")
		}
		return nil
	},
}

var describeCmd = &cobra.Command{
	Use:   "describe <name>",
	Short: "Show profile details",
	Args:  exactlyOneArg("profile name required. Usage: apm describe <name>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return err
		}

		name := args[0]
		if err := validateProfileName(name); err != nil {
			return fmt.Errorf("invalid profile name: %w", err)
		}
		log.Printf("describe: loading profile '%s'", name)
		info, err := getProfile(cfg, name)
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
			if err := json.Unmarshal(data, &settings); err != nil {
				log.Printf("describe: failed to parse %s: %v", settingsPath, err)
			} else if len(settings) > 0 {
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
		} else {
			log.Printf("describe: failed to read %s: %v", settingsPath, err)
		}

		// Show managed dirs content
		for _, dir := range managedDirs {
			dirPath := filepath.Join(info.Dir, dir)
			entries, err := os.ReadDir(dirPath)
			if err != nil {
				log.Printf("describe: failed to read dir %s: %v", dirPath, err)
				continue
			}
			if len(entries) == 0 {
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

var editCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit a profile's settings.json in $EDITOR",
	Args:  exactlyOneArg("profile name required. Usage: apm edit <name>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		name := args[0]
		if err := validateProfileName(name); err != nil {
			return fmt.Errorf("invalid profile name: %w", err)
		}
		if !profileExists(cfg, name) {
			return fmt.Errorf("profile '%s' not found. Run 'apm ls' to see available profiles", name)
		}

		settingsPath := filepath.Join(cfg.ProfileDir(name), "settings.json")

		// Find editor: VISUAL > EDITOR > vi
		editor := strings.TrimSpace(os.Getenv("VISUAL"))
		if editor == "" {
			editor = strings.TrimSpace(os.Getenv("EDITOR"))
		}
		if editor == "" {
			editor = "vi"
		}

		log.Printf("edit: opening '%s' with editor '%s'", settingsPath, editor)

		// Use sh -c to let the shell parse the editor command.
		// This correctly handles quoted paths with spaces (e.g.,
		// EDITOR='"/Applications/Sublime Text.app/.../subl" -w')
		// and multi-word values like "code --wait".
		// "$@" safely passes the file path as a positional argument.
		editorCmd := exec.Command("sh", "-c", editor+` "$@"`, "--", settingsPath)
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		if err := editorCmd.Run(); err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}

		// After editor exits, validate
		if err := validateSettingsJSON(settingsPath); err != nil {
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
			if err := generateProfile(cfg, name); err != nil {
				return fmt.Errorf("regenerating profile: %w", err)
			}
			fmt.Println("Profile regenerated.")
		}

		return nil
	},
}

var lsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List all profiles",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		profiles, err := listProfiles(cfg)
		if err != nil {
			return fmt.Errorf("listing profiles: %w", err)
		}

		log.Printf("ls: found %d profiles", len(profiles))

		if len(profiles) == 0 {
			fmt.Println("No profiles. Create one with: apm create --current")
			return nil
		}

		cf, err := cfg.readConfigFile()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		shellProfile := os.Getenv("APM_PROFILE")

		for _, p := range profiles {
			marker := "  "
			status := ""

			if p.Meta.Name == shellProfile {
				marker = "* "
				status = " (active)"
			} else if p.Meta.Name == cf.ActiveProfile {
				marker = "* "
				status = " (active)"
			} else if p.Meta.Name == cf.DefaultProfile {
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

// errNoActiveProfile is a sentinel error for "no profile active".
// We use a specific type so callers (like statusline scripts) can rely on
// exit code 1 without Cobra printing usage or error text.
type errNoActiveProfile struct{}

func (errNoActiveProfile) Error() string { return "no active profile" }

// isNoActiveProfile reports whether err is the sentinel for "no active profile".
// Used by main.go to exit silently with code 1.
func isNoActiveProfile(err error) bool {
	var target errNoActiveProfile
	return errors.As(err, &target)
}

var currentCmd = &cobra.Command{
	Use:   "current",
	Short: "Output the active profile name",
	Long:  "For statusline scripts. Outputs just the name, nothing else.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check env var first (per-shell override)
		if name := os.Getenv("APM_PROFILE"); name != "" {
			log.Printf("current: from env APM_PROFILE=%s", name)
			fmt.Println(name)
			return nil
		}

		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Read config once for both active and default profile
		cf, err := cfg.readConfigFile()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}

		if cf.ActiveProfile != "" {
			log.Printf("current: from active_profile=%s", cf.ActiveProfile)
			fmt.Println(cf.ActiveProfile)
			return nil
		}

		if cf.DefaultProfile != "" {
			fmt.Println(cf.DefaultProfile)
			return nil
		}

		// No profile active — return sentinel error (exit code 1, no noise)
		return errNoActiveProfile{}
	},
}

var initCmd = &cobra.Command{
	Use:   "init <bash|zsh>",
	Short: "Output shell integration code",
	Long:  "Add 'eval \"$(apm init bash)\"' to your .bashrc or .zshrc",
	Args:  exactlyOneArg("shell type required. Usage: apm init <bash|zsh>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		shellType := args[0]
		script, err := shellInitScript(shellType)
		if err != nil {
			return err
		}
		fmt.Print(script)
		return nil
	},
}

// Flag var — also listed in resetFlags() in cli_commands_test.go.
var regenAll bool

var regenerateCmd = &cobra.Command{
	Use:     "regenerate [name]",
	Aliases: []string{"regen"},
	Short:   "Rebuild generated profile directories",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return err
		}

		if regenAll {
			log.Printf("regenerate: regenerating all profiles")
			profiles, err := listProfiles(cfg)
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				fmt.Println("No profiles to regenerate.")
				return nil
			}
			for _, p := range profiles {
				if err := generateProfile(cfg, p.Meta.Name); err != nil {
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

		if !profileExists(cfg, name) {
			return fmt.Errorf("profile '%s' not found. Run 'apm ls' to see available profiles", name)
		}

		if err := generateProfile(cfg, name); err != nil {
			return err
		}
		fmt.Printf("Regenerated '%s'\n", name)
		return nil
	},
}

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Snapshot current ~/.claude to backup",
	Long: `Copies the current ~/.claude state (auth, settings, history) to the APM backup location.

A backup is automatically created when you first activate a profile (e.g. apm create --current).
Use this command to update the backup at any time.`,
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.EnsureDirs(); err != nil {
			return fmt.Errorf("ensuring directories: %w", err)
		}

		logInfo("Backing up ~/.claude ...")
		if err := backupClaude(cfg); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}

		logSuccess("Backup saved to %s", cfg.BackupDir())
		return nil
	},
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "verbose logging to stderr")
	rootCmd.PersistentFlags().StringVar(&configDir, "config-dir", "", "override config directory (default: ~/.config/apm)")
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	// Command-specific flags
	createCmd.Flags().BoolVar(&createCurrent, "current", false, "import settings from current ~/.claude")
	createCmd.Flags().StringVar(&createFrom, "from", "", "copy from another profile")
	createCmd.Flags().StringVar(&createDesc, "description", "", "profile description")
	useCmd.Flags().BoolVar(&useGlobal, "global", false, "set as default for all new shells")
	useCmd.Flags().Bool("unset", false, "deactivate profile in current shell")
	deleteCmd.Flags().BoolVar(&deleteForce, "force", false, "delete even if profile is active")
	regenerateCmd.Flags().BoolVar(&regenAll, "all", false, "regenerate all profiles")

	// Register commands
	rootCmd.AddCommand(createCmd, useCmd, deleteCmd, describeCmd, editCmd, lsCmd, currentCmd, initCmd, regenerateCmd, backupCmd)
}
