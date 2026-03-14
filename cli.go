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
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// ANSI color helpers for stderr messages.
// Cleared to empty strings when color should be disabled.
var (
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorReset  = "\033[0m"
)

// shouldDisableColor checks NO_COLOR, TERM=dumb, and stderr TTY status.
func shouldDisableColor() bool {
	if os.Getenv("FORCE_COLOR") != "" {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return true
	}
	fi, err := os.Stderr.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

// initColor disables ANSI colors when appropriate.
func initColor() {
	if shouldDisableColor() {
		colorGreen = ""
		colorYellow = ""
		colorRed = ""
		colorCyan = ""
		colorBold = ""
		colorReset = ""
	}
}

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
		initColor()
		if debug {
			log.SetOutput(os.Stderr)
			log.SetFlags(log.Ltime | log.Lshortfile)
			log.Println("debug mode enabled")
		} else {
			log.SetOutput(io.Discard)
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

// runStatus displays a compact status dashboard when `apm` is run with no subcommand.
func runStatus() error {
	cfg, err := loadConfig(configDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	profiles, err := listProfiles(cfg)
	if err != nil {
		return fmt.Errorf("listing profiles: %w", err)
	}

	// First-time user: show onboarding guidance
	if len(profiles) == 0 {
		fmt.Fprintf(os.Stderr, "No profiles found.\n\n")
		fmt.Fprintf(os.Stderr, "Get started:\n")
		fmt.Fprintf(os.Stderr, "  apm create --current    Import your current ~/.claude settings\n")
		fmt.Fprintf(os.Stderr, "  apm create my-profile   Create a fresh profile\n\n")
		fmt.Fprintf(os.Stderr, "Learn more: apm --help\n")
		return nil
	}

	// Read config for active/default profile
	cf, err := cfg.readConfigFile()
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	// Determine active profile (shell env > default_profile)
	shellProfile := os.Getenv(envAPMProfile)
	activeProfile := shellProfile
	activeSource := "shell"
	if activeProfile == "" && cf.DefaultProfile != "" {
		activeProfile = cf.DefaultProfile
		activeSource = "default"
	}

	// Display active profile
	if activeProfile != "" {
		fmt.Fprintf(os.Stderr, colorBold+"Active: "+colorReset+colorGreen+"%s"+colorReset+" (%s)\n", activeProfile, activeSource)
	} else {
		fmt.Fprint(os.Stderr, colorBold+"Active: "+colorReset+colorYellow+"none"+colorReset+"\n")
	}

	// Display profiles
	fmt.Fprintf(os.Stderr, "\n")
	for _, p := range profiles {
		marker := "  "
		status := ""

		if p.Meta.Name == activeProfile {
			marker = colorGreen + "* " + colorReset
			status = colorCyan + " (active)" + colorReset
		} else if p.Meta.Name == cf.DefaultProfile {
			marker = colorCyan + "* " + colorReset
			status = " (global default)"
		}

		desc := ""
		if p.Meta.Description != "" {
			desc = " - " + p.Meta.Description
		}

		fmt.Fprintf(os.Stderr, "%s%s%s%s\n", marker, p.Meta.Name, desc, status)
	}

	return nil
}

// Flag vars — also listed in resetFlags() in cli_commands_test.go.
var (
	createFrom    string
	createCurrent bool
	createDesc    string
)

// confirmPrompt prints a [y/N] prompt to stderr and returns true if the user
// answers "y" or "yes". Returns false on EOF, empty input, or any other answer.
func confirmPrompt(msg string) bool {
	fmt.Fprint(os.Stderr, msg)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

// confirmOverwrite prompts the user to confirm overwriting an existing profile.
var confirmOverwrite = func(name string) bool {
	return confirmPrompt(fmt.Sprintf("Profile '%s' already exists. Overwrite? [y/N]: ", name))
}

// confirmNuke prompts the user to confirm a destructive nuke operation.
var confirmNuke = func() bool {
	return confirmPrompt("This will permanently remove all APM data. Continue? [y/N]: ")
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
	Use:        "create [name]",
	Short:      "Create a new profile (defaults to 'default')",
	SuggestFor: []string{"new", "add"},
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

		// Smart first-run: auto-import from ~/.claude when no profiles exist
		// and user didn't explicitly pass --from or --current
		if source == "" {
			profiles, _ := listProfiles(cfg)
			if len(profiles) == 0 {
				claudeSettings := filepath.Join(cfg.ClaudeDir, settingsFile)
				if _, statErr := os.Stat(claudeSettings); statErr == nil {
					source = "current"
					logInfo("Importing settings from %s", cfg.ClaudeDir)
				}
			}
		}

		log.Printf("create: creating profile '%s' (source=%q, desc=%q)", name, source, createDesc)

		// If profile already exists, prompt for overwrite
		if profileExists(cfg, name) {
			if !confirmOverwrite(name) {
				fmt.Fprintf(os.Stderr, "Aborted.\n")
				return nil
			}

			if err := deleteProfile(cfg, name, true); err != nil {
				return fmt.Errorf("removing existing profile: %w", err)
			}
		}

		if err := createProfile(cfg, name, source, createDesc); err != nil {
			return fmt.Errorf("creating profile: %w", err)
		}

		logSuccess("Created profile '%s'", name)

		// Generate first so settings.json, symlinks (statusline.sh, skills/,
		// etc.) exist before seeding runtime state. seedRuntimeState skips
		// items that already exist, so this ordering is safe.
		if err := generateProfile(cfg, name); err != nil {
			return fmt.Errorf("generating profile: %w", err)
		}

		// Seed runtime state and copy .claude.json for --current profiles
		if source == "current" {
			genDir := cfg.GeneratedProfileDir(name)
			if err := seedRuntimeState(cfg.ClaudeDir, genDir); err != nil {
				return fmt.Errorf("seeding runtime state: %w", err)
			}
			if err := copyClaudeJSON(genDir); err != nil {
				return fmt.Errorf("copying .claude.json: %w", err)
			}
		}

		// Auto-set default if profile is named "default"
		if name == "default" {
			if err := cfg.SetDefaultProfile(name); err != nil {
				return fmt.Errorf("setting default: %w", err)
			}
		}

		if source == "" {
			fmt.Fprintf(os.Stderr, "Edit it with: apm edit %s\n", name)
		}
		return nil
	},
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
	Use:        "use <profile>",
	Short:      "Switch to a profile",
	Long:       "Per-shell by default. Use --global to set default for new shells.",
	SuggestFor: []string{"switch", "activate", "select", "set"},
	Args:       cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		isTTY := isStdoutTTY()

		// Handle --unset
		if len(args) == 0 {
			unset, err := cmd.Flags().GetBool("unset")
			if err != nil {
				return fmt.Errorf("reading --unset flag: %w", err)
			}
			if unset {
				// Read active profile from env (this shell's profile, not config)
				prevActive := os.Getenv(envAPMProfile)

				if err := deactivateProfile(cfg); err != nil {
					return fmt.Errorf("deactivating: %w", err)
				}
				if isTTY {
					fmt.Fprintf(os.Stderr, "To deactivate in this shell, run:\n")
					fmt.Fprintf(os.Stderr, "  eval \"$(apm use --unset)\"\n")
				} else {
					// Unset profile env vars from the previously active profile
					if prevActive != "" {
						genDir := cfg.GeneratedProfileDir(prevActive)
						if unsets, uErr := readEnvUnsets(genDir); uErr != nil {
							log.Printf("use: warning: reading env for unset: %v", uErr)
						} else {
							for _, u := range unsets {
								fmt.Println(u)
							}
						}
					}
					fmt.Println("unset APM_PROFILE")
					fmt.Println("unset CLAUDE_CONFIG_DIR")
				}
				return nil
			}
			return fmt.Errorf("profile name required. Use 'apm use <profile>' or 'apm use --unset'")
		}

		name := args[0]
		if err := requireProfile(cfg, name); err != nil {
			return err
		}
		log.Printf("use: switching to profile '%s' (global=%v)", name, useGlobal)

		// Global: also update config.yaml
		if useGlobal {
			if err := cfg.SetDefaultProfile(name); err != nil {
				return fmt.Errorf("setting default: %w", err)
			}
			log.Printf("use: set global default to '%s'", name)
		}

		// Activate the profile (records in config, generates if needed)
		if err := activateProfile(cfg, name); err != nil {
			return fmt.Errorf("activating profile: %w", err)
		}

		genDir := cfg.GeneratedProfileDir(name)

		if isTTY {
			if useGlobal {
				fmt.Fprintf(os.Stderr, "Global default set to '%s'.\n", name)
				fmt.Fprintf(os.Stderr, "New shells will auto-activate this profile.\n")
			} else {
				logSuccess("Switched to '%s'", name)

				// Detect shell and give targeted guidance
				shellName := filepath.Base(os.Getenv("SHELL"))
				if shellName != "bash" && shellName != "zsh" {
					shellName = "bash"
				}
				rcFile := "~/.bashrc"
				if shellName == "zsh" {
					rcFile = "~/.zshrc"
				}

				fmt.Fprintf(os.Stderr, "\n  Add shell integration to %s:\n", rcFile)
				fmt.Fprintf(os.Stderr, "    eval \"$(apm init %s)\"\n", shellName)
			}
		} else {
			// Unset env vars from previous profile before exporting new ones
			if prev := os.Getenv(envAPMProfile); prev != "" && prev != name {
				prevGenDir := cfg.GeneratedProfileDir(prev)
				if unsets, uErr := readEnvUnsets(prevGenDir); uErr != nil {
					log.Printf("use: warning: reading env for unset: %v", uErr)
				} else {
					for _, u := range unsets {
						fmt.Println(u)
					}
				}
			}
			fmt.Printf("export APM_PROFILE='%s'\n", shellQuote(name))
			fmt.Printf("export CLAUDE_CONFIG_DIR='%s'\n", shellQuote(genDir))
			// Export per-profile environment variables
			if exports, eErr := readEnvExports(genDir); eErr != nil {
				log.Printf("use: warning: reading env for export: %v", eErr)
			} else {
				for _, e := range exports {
					fmt.Println(e)
				}
			}
		}

		return nil
	},
}

// Flag var — also listed in resetFlags() in cli_commands_test.go.
var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:        "delete <name>",
	Aliases:    []string{"rm"},
	Short:      "Delete a profile",
	SuggestFor: []string{"remove"},
	Args:       exactlyOneArg("profile name required. Usage: apm delete <name>"),
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

		// Read config once for checks
		cf, err := cfg.readConfigFile()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		wasDefault := cf.DefaultProfile == name

		if err := deleteProfile(cfg, name, deleteForce); err != nil {
			return fmt.Errorf("deleting profile: %w", err)
		}

		logSuccess("Deleted profile '%s'", name)

		if wasDefault {
			logWarn("Global default profile has been cleared.")
		}

		if os.Getenv(envAPMProfile) == name {
			logWarn("'%s' is still set in this shell.", name)
			logInfo("Run: eval \"$(apm use --unset)\"")
		}
		return nil
	},
}

// runDescribe shows detailed info for a single profile. Used by both
// `apm describe <name>` and `apm ls <name>`.
func runDescribe(cfg *Config, name string) error {
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
	settingsPath := filepath.Join(info.Dir, settingsFile)
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
}

var describeCmd = &cobra.Command{
	Use:   "describe <name>",
	Short: "Show profile details (alias for 'ls <name>')",
	Args:  exactlyOneArg("profile name required. Usage: apm describe <name>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return err
		}
		return runDescribe(cfg, args[0])
	},
}

// Flag var — also listed in resetFlags() in cli_commands_test.go.
var editEnv bool

var editCmd = &cobra.Command{
	Use:        "edit <name>",
	Short:      "Edit a profile's settings or env file in $EDITOR",
	SuggestFor: []string{"modify", "change", "config", "configure"},
	Args:       exactlyOneArg("profile name required. Usage: apm edit <name>"),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		name := args[0]
		if err := requireProfile(cfg, name); err != nil {
			return err
		}

		targetPath := filepath.Join(cfg.ProfileDir(name), settingsFile)
		if editEnv {
			targetPath = filepath.Join(cfg.ProfileDir(name), envFileName)
			// Create env file with header if missing
			if _, statErr := os.Stat(targetPath); errors.Is(statErr, os.ErrNotExist) {
				header := "# Environment variables for profile '" + name + "'\n# Format: KEY=VALUE\n"
				if err := os.WriteFile(targetPath, []byte(header), 0o644); err != nil {
					return fmt.Errorf("creating env file: %w", err)
				}
			}
		}

		// Find editor: VISUAL > EDITOR > vi
		editor := strings.TrimSpace(os.Getenv("VISUAL"))
		if editor == "" {
			editor = strings.TrimSpace(os.Getenv("EDITOR"))
		}
		if editor == "" {
			editor = "vi"
		}

		log.Printf("edit: opening '%s' with editor '%s'", targetPath, editor)

		// Use sh -c to let the shell parse the editor command.
		editorCmd := exec.Command("sh", "-c", editor+` "$@"`, "--", targetPath)
		editorCmd.Stdin = os.Stdin
		editorCmd.Stdout = os.Stdout
		editorCmd.Stderr = os.Stderr

		if err := editorCmd.Run(); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return fmt.Errorf("editor '%s' not found. Set $EDITOR (e.g., export EDITOR=nano)", editor)
			}
			return fmt.Errorf("editor failed: %w", err)
		}

		// After editor exits, validate the edited file
		if editEnv {
			if _, err := parseEnvFile(targetPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				fmt.Fprintf(os.Stderr, "Run 'apm edit --env %s' to fix it.\n", name)
				return nil // don't regenerate broken env
			}
		} else {
			if err := validateSettingsJSON(targetPath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
				fmt.Fprintf(os.Stderr, "Run 'apm edit %s' to fix it.\n", name)
				return nil // don't regenerate broken settings
			}
		}

		// Regenerate if this profile is active (in shell, config, or as global default)
		active := os.Getenv(envAPMProfile)
		cf, err := cfg.readConfigFile()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		if active == name || cf.DefaultProfile == name {
			if err := generateProfile(cfg, name); err != nil {
				return fmt.Errorf("regenerating profile: %w", err)
			}
			logSuccess("Profile regenerated")
		}

		return nil
	},
}

var lsCmd = &cobra.Command{
	Use:        "ls [name]",
	Aliases:    []string{"list"},
	Short:      "List profiles, or show details for one",
	SuggestFor: []string{"show", "profiles", "info", "status"},
	Args:       cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// If a name is given, show details (same as describe)
		if len(args) == 1 {
			return runDescribe(cfg, args[0])
		}

		profiles, err := listProfiles(cfg)
		if err != nil {
			return fmt.Errorf("listing profiles: %w", err)
		}

		log.Printf("ls: found %d profiles", len(profiles))

		if len(profiles) == 0 {
			fmt.Fprintln(os.Stderr, "No profiles. Create one with: apm create")
			return nil
		}

		cf, err := cfg.readConfigFile()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}
		shellProfile := os.Getenv(envAPMProfile)

		for _, p := range profiles {
			marker := "  "
			status := ""

			if p.Meta.Name == shellProfile {
				marker = "* "
				status = " (active)"
			} else if p.Meta.Name == cf.DefaultProfile {
				marker = "* "
				status = " (default)"
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
		if name := os.Getenv(envAPMProfile); name != "" {
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

var regenerateCmd = &cobra.Command{
	Use:     "regenerate [name]",
	Aliases: []string{"regen"},
	Short:   "Rebuild generated profile directories",
	Hidden:  true,
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return err
		}

		// If a name is given, regenerate just that profile
		if len(args) == 1 {
			name := args[0]
			log.Printf("regenerate: regenerating profile '%s'", name)

			if err := requireProfile(cfg, name); err != nil {
				return err
			}

			if err := generateProfile(cfg, name); err != nil {
				return err
			}
			logSuccess("Regenerated '%s'", name)
			return nil
		}

		// No args = regenerate all profiles
		log.Printf("regenerate: regenerating all profiles")
		profiles, err := listProfiles(cfg)
		if err != nil {
			return err
		}
		if len(profiles) == 0 {
			fmt.Fprintln(os.Stderr, "No profiles to regenerate.")
			return nil
		}

		// Determine active profile so we can regenerate it last
		activeProfile := os.Getenv(envAPMProfile)
		if activeProfile == "" {
			cf, _ := cfg.readConfigFile()
			if cf != nil {
				activeProfile = cf.DefaultProfile
			}
		}
		var activeIdx = -1
		for i, p := range profiles {
			if p.Meta.Name == activeProfile {
				activeIdx = i
				break
			}
		}

		for i, p := range profiles {
			if i == activeIdx {
				continue // defer active profile to last
			}
			if err := generateProfile(cfg, p.Meta.Name); err != nil {
				return fmt.Errorf("regenerating '%s': %w", p.Meta.Name, err)
			}
			logSuccess("Regenerated '%s'", p.Meta.Name)
		}

		// Regenerate active profile last with a warning
		if activeIdx >= 0 {
			logWarn("Regenerating active profile '%s'. Ensure Claude CLI is not running.", activeProfile)
			if err := generateProfile(cfg, activeProfile); err != nil {
				return fmt.Errorf("regenerating '%s': %w", activeProfile, err)
			}
			logSuccess("Regenerated '%s'", activeProfile)
		}

		return nil
	},
}

var diffCmd = &cobra.Command{
	Use:   "diff <profile-a> <profile-b>",
	Short: "Show differences between two profiles",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		nameA, nameB := args[0], args[1]
		for _, name := range []string{nameA, nameB} {
			if err := requireProfile(cfg, name); err != nil {
				return err
			}
		}

		// Compare settings.json
		settingsA, err := loadJSON(filepath.Join(cfg.ProfileDir(nameA), settingsFile))
		if err != nil {
			return err
		}
		settingsB, err := loadJSON(filepath.Join(cfg.ProfileDir(nameB), settingsFile))
		if err != nil {
			return err
		}

		hasDiff := false

		// Find keys in A but not B, in both, and in B but not A
		allKeys := make(map[string]bool)
		for k := range settingsA {
			allKeys[k] = true
		}
		for k := range settingsB {
			allKeys[k] = true
		}

		sorted := make([]string, 0, len(allKeys))
		for k := range allKeys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)

		if len(allKeys) > 0 {
			fmt.Fprint(os.Stderr, colorBold+"Settings:"+colorReset+"\n")
		}
		for _, key := range sorted {
			valA, inA := settingsA[key]
			valB, inB := settingsB[key]

			if inA && !inB {
				fmt.Fprintf(os.Stderr, colorRed+"  - %s: %v"+colorReset+" (only in %s)\n", key, formatVal(valA), nameA)
				hasDiff = true
			} else if !inA && inB {
				fmt.Fprintf(os.Stderr, colorGreen+"  + %s: %v"+colorReset+" (only in %s)\n", key, formatVal(valB), nameB)
				hasDiff = true
			} else if fmt.Sprintf("%v", valA) != fmt.Sprintf("%v", valB) {
				fmt.Fprintf(os.Stderr, colorYellow+"  ~ %s:"+colorReset+" %v → %v\n", key, formatVal(valA), formatVal(valB))
				hasDiff = true
			}
		}

		// Compare env files
		envA, _ := parseEnvFile(filepath.Join(cfg.ProfileDir(nameA), envFileName))
		envB, _ := parseEnvFile(filepath.Join(cfg.ProfileDir(nameB), envFileName))
		if len(envA) > 0 || len(envB) > 0 {
			allEnv := make(map[string]bool)
			for k := range envA {
				allEnv[k] = true
			}
			for k := range envB {
				allEnv[k] = true
			}
			envKeys := sortedKeys(func() map[string]string {
				m := make(map[string]string, len(allEnv))
				for k := range allEnv {
					m[k] = ""
				}
				return m
			}())

			envDiff := false
			for _, key := range envKeys {
				vA, inA := envA[key]
				vB, inB := envB[key]
				if inA && !inB {
					if !envDiff {
						fmt.Fprint(os.Stderr, colorBold+"\nEnv vars:"+colorReset+"\n")
						envDiff = true
					}
					fmt.Fprintf(os.Stderr, colorRed+"  - %s=%s"+colorReset+" (only in %s)\n", key, vA, nameA)
					hasDiff = true
				} else if !inA && inB {
					if !envDiff {
						fmt.Fprint(os.Stderr, colorBold+"\nEnv vars:"+colorReset+"\n")
						envDiff = true
					}
					fmt.Fprintf(os.Stderr, colorGreen+"  + %s=%s"+colorReset+" (only in %s)\n", key, vB, nameB)
					hasDiff = true
				} else if vA != vB {
					if !envDiff {
						fmt.Fprint(os.Stderr, colorBold+"\nEnv vars:"+colorReset+"\n")
						envDiff = true
					}
					fmt.Fprintf(os.Stderr, colorYellow+"  ~ %s:"+colorReset+" %s → %s\n", key, vA, vB)
					hasDiff = true
				}
			}
		}

		if !hasDiff {
			fmt.Fprintln(os.Stderr, "No differences found.")
		}
		return nil
	},
}

// formatVal formats a JSON value for display.
func formatVal(v any) string {
	switch val := v.(type) {
	case map[string]any:
		b, _ := json.Marshal(val)
		return string(b)
	case []any:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return fmt.Sprintf("%v", v)
	}
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check for common configuration issues",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		ok := true
		pass := func(msg string) { fmt.Fprintf(os.Stderr, colorGreen+"  [ok] "+colorReset+"%s\n", msg) }
		warn := func(msg string) { fmt.Fprintf(os.Stderr, colorYellow+"  [!!] "+colorReset+"%s\n", msg); ok = false }

		cf, err := cfg.readConfigFile()
		if err != nil {
			return fmt.Errorf("reading config: %w", err)
		}

		profiles, _ := listProfiles(cfg)
		profileSet := make(map[string]bool, len(profiles))
		for _, p := range profiles {
			profileSet[p.Meta.Name] = true
		}

		// Check 1: Shell integration
		shellProfile := os.Getenv(envAPMProfile)
		if shellProfile != "" {
			pass(fmt.Sprintf("Shell integration active (APM_PROFILE=%s)", shellProfile))
		} else if cf.DefaultProfile != "" {
			pass(fmt.Sprintf("Default profile '%s' set (new shells will auto-activate)", cf.DefaultProfile))
		} else {
			pass("No profile active")
		}

		// Check 2: Default profile exists
		if cf.DefaultProfile != "" {
			if profileSet[cf.DefaultProfile] {
				pass(fmt.Sprintf("Default profile '%s' exists", cf.DefaultProfile))
			} else {
				warn(fmt.Sprintf("Default profile '%s' not found in profiles/", cf.DefaultProfile))
			}
		}

		// Check 3: Generated dir for active profile
		activeCheck := shellProfile
		if activeCheck == "" {
			activeCheck = cf.DefaultProfile
		}
		if activeCheck != "" && profileSet[activeCheck] {
			genDir := cfg.GeneratedProfileDir(activeCheck)
			if _, statErr := os.Stat(genDir); statErr == nil {
				pass("Generated directory present for active profile")
			} else {
				warn(fmt.Sprintf("Generated directory missing for '%s'. Run: apm regenerate %s", activeCheck, activeCheck))
			}
		}

		// Check 5: Orphaned generated dirs
		genEntries, _ := os.ReadDir(cfg.GeneratedDir)
		for _, e := range genEntries {
			if e.IsDir() && !profileSet[e.Name()] {
				warn(fmt.Sprintf("Orphaned generated dir: '%s' (no matching profile)", e.Name()))
			}
		}

		// Check 6: Settings JSON validity
		settingsOK := true
		for _, p := range profiles {
			settingsPath := filepath.Join(p.Dir, settingsFile)
			if err := validateSettingsJSON(settingsPath); err != nil {
				warn(fmt.Sprintf("Invalid settings.json in profile '%s': %v", p.Meta.Name, err))
				settingsOK = false
			}
		}
		if settingsOK && len(profiles) > 0 {
			pass("All settings.json files valid")
		}

		if ok {
			fmt.Fprintln(os.Stderr, "\nNo issues found.")
		}
		return nil
	},
}

var renameCmd = &cobra.Command{
	Use:        "rename <old> <new>",
	Aliases:    []string{"mv"},
	Short:      "Rename a profile",
	SuggestFor: []string{"move"},
	Args:       cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		oldName, newName := args[0], args[1]

		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if err := requireProfile(cfg, oldName); err != nil {
			return err
		}
		if err := validateProfileName(newName); err != nil {
			return fmt.Errorf("invalid new name: %w", err)
		}
		if profileExists(cfg, newName) {
			return fmt.Errorf("profile '%s' already exists", newName)
		}

		// Rename profile directory
		if err := os.Rename(cfg.ProfileDir(oldName), cfg.ProfileDir(newName)); err != nil {
			return fmt.Errorf("renaming profile directory: %w", err)
		}

		// Update profile.yaml with new name
		meta, err := readMeta(cfg.ProfileDir(newName))
		if err != nil {
			return fmt.Errorf("reading profile metadata: %w", err)
		}
		meta.Name = newName
		if err := writeMeta(cfg.ProfileDir(newName), meta); err != nil {
			return fmt.Errorf("updating profile metadata: %w", err)
		}

		// Rename the generated directory to preserve runtime state (auth tokens,
		// session history, etc.), then regenerate managed items in-place so
		// symlinks point to the new profile path.
		oldGen := cfg.GeneratedProfileDir(oldName)
		newGen := cfg.GeneratedProfileDir(newName)
		if _, statErr := os.Stat(oldGen); statErr == nil {
			if err := os.Rename(oldGen, newGen); err != nil {
				return fmt.Errorf("renaming generated directory: %w", err)
			}
		}
		if err := generateProfile(cfg, newName); err != nil {
			return fmt.Errorf("regenerating profile: %w", err)
		}

		// Update config.yaml references (using locking for safe concurrent access)
		if err := cfg.lockedConfigUpdate(func(cf *ConfigFile) error {
			if cf.DefaultProfile == oldName {
				cf.DefaultProfile = newName
			}
			return nil
		}); err != nil {
			return fmt.Errorf("updating config: %w", err)
		}

		logSuccess("Renamed '%s' → '%s'", oldName, newName)

		if os.Getenv(envAPMProfile) == oldName {
			logWarn("Shell still references '%s'.", oldName)
			logInfo("Run: eval \"$(apm use %s)\"", newName)
		}
		return nil
	},
}

// Flag var — also listed in resetFlags() in cli_commands_test.go.
var nukeForce bool

var nukeCmd = &cobra.Command{
	Use:   "nuke",
	Short: "Remove all profiles and restore original ~/.claude",
	Long: `Removes all profiles and generated data. If a profile is active,
its generated directory is flattened into ~/.claude so that auth tokens
and runtime state survive. The common directory is preserved.

This is irreversible. Use --force to skip the confirmation prompt.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig(configDir)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		profiles, _ := listProfiles(cfg)
		logWarn("This will permanently remove:")
		if len(profiles) > 0 {
			logInfo("  %d profile(s)", len(profiles))
		}
		logInfo("  All generated profile data")
		logInfo("  Common directory will be preserved")

		if !nukeForce {
			if !confirmNuke() {
				fmt.Fprintf(os.Stderr, "Aborted.\n")
				return nil
			}
		}

		// Collect env unsets BEFORE nuking (nuke removes the generated dir)
		var envUnsets []string
		if activeName := os.Getenv(envAPMProfile); activeName != "" {
			genDir := cfg.GeneratedProfileDir(activeName)
			if unsets, uErr := readEnvUnsets(genDir); uErr != nil {
				log.Printf("nuke: warning: reading env for unset: %v", uErr)
			} else {
				envUnsets = unsets
			}
		}

		if err := nukeAPM(cfg); err != nil {
			return fmt.Errorf("nuke failed: %w", err)
		}

		logSuccess("All profiles removed.")

		if os.Getenv(envAPMProfile) != "" {
			for _, u := range envUnsets {
				fmt.Println(u)
			}
			fmt.Println("unset APM_PROFILE")
			fmt.Println("unset CLAUDE_CONFIG_DIR")
		}

		return nil
	},
}

// completeProfileNames returns a cobra completion function that suggests profile names.
func completeProfileNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := loadConfig(configDir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	profiles, err := listProfiles(cfg)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		if p.Meta.Description != "" {
			names = append(names, fmt.Sprintf("%s\t%s", p.Meta.Name, p.Meta.Description))
		} else {
			names = append(names, p.Meta.Name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "verbose logging to stderr")
	rootCmd.PersistentFlags().StringVar(&configDir, "config-dir", "", "override config directory (default: ~/.config/apm)")
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
	rootCmd.Version = versionInfo()

	// Command groups for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: "everyday", Title: "Everyday Commands:"},
		&cobra.Group{ID: "manage", Title: "Profile Management:"},
		&cobra.Group{ID: "setup", Title: "Setup:"},
	)
	useCmd.GroupID = "everyday"
	lsCmd.GroupID = "everyday"
	currentCmd.GroupID = "everyday"
	diffCmd.GroupID = "everyday"
	createCmd.GroupID = "manage"
	editCmd.GroupID = "manage"
	describeCmd.GroupID = "manage"
	deleteCmd.GroupID = "manage"
	renameCmd.GroupID = "manage"
	initCmd.GroupID = "setup"
	doctorCmd.GroupID = "setup"
	nukeCmd.GroupID = "setup"

	// Command-specific flags
	createCmd.Flags().BoolVar(&createCurrent, "current", false, "import settings from current ~/.claude")
	createCmd.Flags().StringVar(&createFrom, "from", "", "copy from another profile")
	createCmd.Flags().StringVar(&createDesc, "description", "", "profile description")
	useCmd.Flags().BoolVar(&useGlobal, "global", false, "set as default for all new shells")
	useCmd.Flags().Bool("unset", false, "deactivate profile in current shell")
	editCmd.Flags().BoolVar(&editEnv, "env", false, "edit environment variables instead of settings")
	deleteCmd.Flags().BoolVar(&deleteForce, "force", false, "delete even if profile is active")
	nukeCmd.Flags().BoolVar(&nukeForce, "force", false, "skip confirmation prompt")

	// Shell completion for commands that take profile names
	for _, cmd := range []*cobra.Command{useCmd, describeCmd, editCmd, deleteCmd, regenerateCmd, lsCmd, createCmd, renameCmd} {
		cmd.ValidArgsFunction = completeProfileNames
	}
	_ = createCmd.RegisterFlagCompletionFunc("from", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return completeProfileNames(cmd, nil, toComplete)
	})

	// Register all commands
	rootCmd.AddCommand(createCmd, useCmd, deleteCmd, describeCmd, editCmd, lsCmd, currentCmd, diffCmd, initCmd, regenerateCmd, renameCmd, doctorCmd, nukeCmd)
}

// ---------------------------------------------------------------------------
// Shell init scripts
// ---------------------------------------------------------------------------

// shellInitTemplate is the shared shell integration script.
// %[1]s = open bracket ([ or [[), %[2]s = close bracket (] or ]]),
// %[3]s = shell type ("bash" or "zsh")
const shellInitTemplate = `
# Agent Profile Manager shell integration
apm() {
  # Pass through help flags directly (don't eval help text)
  local arg
  for arg in "$@"; do
    case "$arg" in
      -h|--help|help) command apm "$@"; return ;;
    esac
  done

  case "$1" in
    use|nuke)
      local stdout_file stderr_file
      stdout_file=$(mktemp) stderr_file=$(mktemp)
      command apm "$@" >"$stdout_file" 2>"$stderr_file"
      local rc=$?
      if %[1]s $rc -eq 0 %[2]s; then
        eval "$(cat "$stdout_file")"
        rc=$?
      fi
      cat "$stderr_file" >&2
      rm -f "$stdout_file" "$stderr_file"
      return $rc
      ;;
    *)
      command apm "$@"
      ;;
  esac
}

# Tab completion for profile names
source <(command apm completion %[3]s 2>/dev/null)

# Auto-activate global default on shell startup
_apm_auto_activate() {
  if %[1]s -z "$APM_PROFILE" %[2]s; then
    local profile
    profile=$(command apm current 2>/dev/null)
    if %[1]s -n "$profile" %[2]s; then
      eval "$(command apm use "$profile" 2>/dev/null)"
    fi
  fi
}
_apm_auto_activate
`

var (
	bashInit = fmt.Sprintf(shellInitTemplate, "[", "]", "bash")
	zshInit  = fmt.Sprintf(shellInitTemplate, "[[", "]]", "zsh")
)

// shellInitScript returns the shell integration script for the given shell.
func shellInitScript(shellType string) (string, error) {
	switch shellType {
	case "bash":
		return bashInit, nil
	case "zsh":
		return zshInit, nil
	default:
		return "", fmt.Errorf("unsupported shell: %s (use 'bash' or 'zsh')", shellType)
	}
}
