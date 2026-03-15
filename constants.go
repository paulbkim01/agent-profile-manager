package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// managedDirs are the directories that profiles control (merged via symlinks).
var managedDirs = []string{
	"skills",
	"commands",
	"agents",
}

// managedItemSet is the set of all files/dirs that profiles control.
// These are merged from common + profile during generation.
var managedItemSet = func() map[string]bool {
	s := map[string]bool{"settings.json": true}
	for _, d := range managedDirs {
		s[d] = true
	}
	return s
}()

// claudeHomeDirName is the directory name used to back up ~/.claude during activation.
const claudeHomeDirName = "claude-home"

// reservedNames cannot be used as profile names.
var reservedNames = map[string]bool{
	"common":      true,
	"generated":   true,
	"config":      true,
	"current":     true,
	"state":       true,
	"claude-home": true,
}

// defaultClaudeDir returns ~/.claude.
// This must NOT read CLAUDE_CONFIG_DIR, because that env var points
// to a generated profile dir once a profile is active.
// Use config.yaml claude_dir for non-standard installs.
func defaultClaudeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// defaultAPMDir returns ~/.config/apm (or $XDG_CONFIG_HOME/apm).
func defaultAPMDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "apm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "apm"), nil
}
