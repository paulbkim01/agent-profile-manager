package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// version is set at build time via -ldflags "-X main.version=v0.1.0".
var version = "dev"

// versionInfo returns version string with Go and platform info.
func versionInfo() string {
	return fmt.Sprintf("%s (%s %s/%s)", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// managedDirs are the directories that profiles control (merged via symlinks).
var managedDirs = []string{
	"skills",
	"commands",
	"agents",
	"rules",
}

// managedItemSet is the set of all files/dirs that profiles control.
// These are merged from common + profile during generation.
var managedItemSet = func() map[string]bool {
	s := map[string]bool{
		settingsFile: true,
		apmMetaFile:  true,
		envFileName:  true,
	}
	for _, d := range managedDirs {
		s[d] = true
	}
	return s
}()

// settingsFile is the per-profile settings file.
const settingsFile = "settings.json"

// envFileName is the per-profile environment variable file.
const envFileName = "env"

// profileMetaFile is the per-profile metadata file.
const profileMetaFile = "profile.yaml"

// apmMetaFile is the APM-generated metadata file in generated dirs.
const apmMetaFile = ".apm-meta.json"

// metaKeyInputHash is the key in .apm-meta.json for the input hash.
const metaKeyInputHash = "input_hash"

// envAPMProfile is the environment variable for the active profile name.
const envAPMProfile = "APM_PROFILE"

// envClaudeConfigDir is the environment variable Claude Code reads for its config directory.
const envClaudeConfigDir = "CLAUDE_CONFIG_DIR"

// reservedNames cannot be used as profile names.
var reservedNames = map[string]bool{
	"common":    true,
	"generated": true,
	"config":    true,
	"current":   true,
	"state":     true,
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

// atomicWriteFile writes data to path via a temp file + rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("setting permissions on %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming %s to %s: %w", tmpName, path, err)
	}
	return nil
}

// isSymlink reports whether the given FileInfo represents a symlink.
func isSymlink(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
}

// shellQuote escapes a string for safe use inside POSIX single quotes.
// The standard technique: replace each ' with '\'' (end quote, literal ', start quote).
func shellQuote(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}
