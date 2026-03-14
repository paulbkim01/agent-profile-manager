package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the YAML structure of config.yaml.
type ConfigFile struct {
	DefaultProfile string `yaml:"default_profile,omitempty"`
	ClaudeDir      string `yaml:"claude_dir,omitempty"`     // user override
	ActiveProfile  string `yaml:"active_profile,omitempty"` // currently active profile
}

// Config holds resolved paths and state for the current run.
type Config struct {
	APMDir    string // ~/.config/apm or override
	ClaudeDir string // ~/.claude or config.yaml override

	// Derived paths
	CommonDir    string // APMDir/common
	ProfilesDir  string // APMDir/profiles
	GeneratedDir string // APMDir/generated
	ConfigPath   string // APMDir/config.yaml
}

// readConfigFile reads and parses config.yaml.
// Returns a zero-value ConfigFile if the file does not exist.
func (c *Config) readConfigFile() (*ConfigFile, error) {
	data, err := os.ReadFile(c.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		return &ConfigFile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", c.ConfigPath, err)
	}
	var cf ConfigFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", c.ConfigPath, err)
	}
	return &cf, nil
}

// loadConfig reads config.yaml and resolves all paths.
// apmDirOverride is from --config-dir flag (empty string = use default).
func loadConfig(apmDirOverride string) (*Config, error) {
	apmDir := apmDirOverride
	if apmDir == "" {
		var err error
		apmDir, err = defaultAPMDir()
		if err != nil {
			return nil, fmt.Errorf("determining config directory: %w", err)
		}
	}

	claudeDir, err := defaultClaudeDir()
	if err != nil {
		return nil, fmt.Errorf("determining claude directory: %w", err)
	}

	c := &Config{
		APMDir:       apmDir,
		ClaudeDir:    claudeDir,
		CommonDir:    filepath.Join(apmDir, "common"),
		ProfilesDir:  filepath.Join(apmDir, "profiles"),
		GeneratedDir: filepath.Join(apmDir, "generated"),
		ConfigPath:   filepath.Join(apmDir, "config.yaml"),
	}

	cf, err := c.readConfigFile()
	if err != nil {
		return nil, err
	}
	if cf.ClaudeDir != "" {
		c.ClaudeDir = cf.ClaudeDir
	}

	// Run one-time migration from symlink-based activation
	if err := migrateFromSymlinks(c); err != nil {
		log.Printf("config: migration warning: %v", err)
	}

	log.Printf("config: apm_dir=%s claude_dir=%s", c.APMDir, c.ClaudeDir)
	return c, nil
}

// DefaultProfile reads the default_profile from config.yaml.
// Returns empty string if none set.
func (c *Config) DefaultProfile() (string, error) {
	cf, err := c.readConfigFile()
	if err != nil {
		return "", err
	}
	return cf.DefaultProfile, nil
}

// writeConfigFile atomically writes the ConfigFile to disk.
func (c *Config) writeConfigFile(cf *ConfigFile) error {
	out, err := yaml.Marshal(cf)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	return atomicWriteFile(c.ConfigPath, out, 0o644)
}

// lockedConfigUpdate performs a read-modify-write on config.yaml while holding
// an advisory file lock. This prevents concurrent apm processes from losing
// each other's updates. The lock is held only for the duration of the callback.
func (c *Config) lockedConfigUpdate(fn func(cf *ConfigFile) error) error {
	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	lockPath := c.ConfigPath + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		// If locking fails, fall back to unlocked update
		log.Printf("config: warning: could not open lock file: %v", err)
		return c.unlockedConfigUpdate(fn)
	}
	defer func() {
		lockFile.Close()
		os.Remove(lockPath) // best-effort cleanup of lock file
	}()

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		log.Printf("config: warning: could not acquire lock: %v", err)
		return c.unlockedConfigUpdate(fn)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	return c.unlockedConfigUpdate(fn)
}

// unlockedConfigUpdate performs a read-modify-write without locking.
func (c *Config) unlockedConfigUpdate(fn func(cf *ConfigFile) error) error {
	cf, err := c.readConfigFile()
	if err != nil {
		return err
	}
	if err := fn(cf); err != nil {
		return err
	}
	return c.writeConfigFile(cf)
}

// SetDefaultProfile writes default_profile to config.yaml.
func (c *Config) SetDefaultProfile(name string) error {
	return c.lockedConfigUpdate(func(cf *ConfigFile) error {
		cf.DefaultProfile = name
		return nil
	})
}

// ClearDefaultProfile removes default_profile from config.yaml.
func (c *Config) ClearDefaultProfile() error {
	return c.SetDefaultProfile("")
}

// ProfileDir returns the path for a specific profile.
func (c *Config) ProfileDir(name string) string {
	return filepath.Join(c.ProfilesDir, name)
}

// GeneratedProfileDir returns the generated dir for a specific profile.
func (c *Config) GeneratedProfileDir(name string) string {
	return filepath.Join(c.GeneratedDir, name)
}

// CleanGeneratedDir removes a specific profile's generated directory.
func (c *Config) CleanGeneratedDir(name string) {
	genDir := c.GeneratedProfileDir(name)
	if err := os.RemoveAll(genDir); err != nil {
		log.Printf("config: failed to remove generated dir %s: %v", genDir, err)
	}
}

// EnsureDirs creates the base directory structure if it doesn't exist.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.CommonDir,
		c.ProfilesDir,
		c.GeneratedDir,
	}
	for _, sub := range managedDirs {
		dirs = append(dirs, filepath.Join(c.CommonDir, sub))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}

	// Create common/settings.json if missing (with default statusLine config)
	commonSettings := filepath.Join(c.CommonDir, settingsFile)
	if _, err := os.Stat(commonSettings); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("checking %s: %w", commonSettings, err)
		}
		if err := os.WriteFile(commonSettings, defaultStatusLineSettings(c.CommonDir), 0o644); err != nil {
			return fmt.Errorf("creating %s: %w", commonSettings, err)
		}
	}

	// Create common/statusline.sh if missing; also patches ~/.claude/settings.json
	if err := writeDefaultStatusLine(c.CommonDir, c.ClaudeDir); err != nil {
		return fmt.Errorf("creating default statusline: %w", err)
	}

	// Create .gitignore to prevent accidental commit of OAuth tokens
	gitignorePath := filepath.Join(c.APMDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); errors.Is(err, os.ErrNotExist) {
		content := "# Prevent committing OAuth tokens and session state\ngenerated/*/.claude.json\n"
		if err := os.WriteFile(gitignorePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("creating %s: %w", gitignorePath, err)
		}
	}

	return nil
}
