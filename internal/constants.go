package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

// ManagedItems are the files/dirs that profiles control.
// Everything else in ~/.claude/ gets symlinked into generated dirs.
var ManagedItems = []string{
	"settings.json",
	"skills",
	"commands",
	"agents",
}

// ManagedDirs is the subset of ManagedItems that are directories (merged via symlinks).
var ManagedDirs = []string{
	"skills",
	"commands",
	"agents",
}

// ReservedNames cannot be used as profile names.
var ReservedNames = map[string]bool{
	"common":    true,
	"generated": true,
	"config":    true,
	"default":   true,
	"current":   true,
	"state":     true,
}

// DefaultClaudeDir returns ~/.claude.
// This must NOT read CLAUDE_CONFIG_DIR, because that env var points
// to a generated profile dir once a profile is active.
// Use config.yaml claude_dir for non-standard installs.
func DefaultClaudeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// DefaultAPMDir returns ~/.config/apm (or $XDG_CONFIG_HOME/apm).
func DefaultAPMDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "apm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "apm"), nil
}
