package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the YAML structure of config.yaml.
type ConfigFile struct {
	DefaultProfile string `yaml:"default_profile,omitempty"`
	ClaudeDir      string `yaml:"claude_dir,omitempty"`      // user override
	ActiveProfile  string `yaml:"active_profile,omitempty"`  // currently symlinked profile
	ClaudeHomePath string `yaml:"claude_home,omitempty"`     // where real ~/.claude/ was moved
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
	} else if cf.ClaudeHomePath != "" {
		c.ClaudeDir = cf.ClaudeHomePath
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

// atomicWriteFile writes data to path via a temp file + rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
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

// SetDefaultProfile writes default_profile to config.yaml.
func (c *Config) SetDefaultProfile(name string) error {
	cf, err := c.readConfigFile()
	if err != nil {
		return err
	}
	cf.DefaultProfile = name
	return c.writeConfigFile(cf)
}

// ClearDefaultProfile removes default_profile from config.yaml.
func (c *Config) ClearDefaultProfile() error {
	return c.SetDefaultProfile("")
}

// ActiveProfile reads the active_profile from config.yaml.
func (c *Config) ActiveProfile() (string, error) {
	cf, err := c.readConfigFile()
	if err != nil {
		return "", err
	}
	return cf.ActiveProfile, nil
}

// SetActiveProfile writes active_profile to config.yaml.
func (c *Config) SetActiveProfile(name string) error {
	cf, err := c.readConfigFile()
	if err != nil {
		return err
	}
	cf.ActiveProfile = name
	return c.writeConfigFile(cf)
}

// ClearActiveProfile clears active_profile and claude_home from config.yaml.
func (c *Config) ClearActiveProfile() error {
	cf, err := c.readConfigFile()
	if err != nil {
		return err
	}
	cf.ActiveProfile = ""
	cf.ClaudeHomePath = ""
	return c.writeConfigFile(cf)
}

// SetClaudeHome writes claude_home to config.yaml.
func (c *Config) SetClaudeHome(path string) error {
	cf, err := c.readConfigFile()
	if err != nil {
		return err
	}
	cf.ClaudeHomePath = path
	return c.writeConfigFile(cf)
}

// ClaudeHome reads the claude_home path from config.yaml.
func (c *Config) ClaudeHome() (string, error) {
	cf, err := c.readConfigFile()
	if err != nil {
		return "", err
	}
	return cf.ClaudeHomePath, nil
}

// IsActivated reports whether a profile is currently symlink-activated.
func (c *Config) IsActivated() (bool, error) {
	cf, err := c.readConfigFile()
	if err != nil {
		return false, err
	}
	return cf.ActiveProfile != "", nil
}

// BackupDir returns the path to the backed-up ~/.claude directory.
func (c *Config) BackupDir() string {
	return filepath.Join(c.APMDir, claudeHomeDirName)
}

// BackupExternalDir returns the path to the backed-up ~/.claude.json.
func (c *Config) BackupExternalDir() string {
	return filepath.Join(c.APMDir, "claude-home-external")
}

// ExternalStateDir returns the per-profile external state directory.
func (c *Config) ExternalStateDir(name string) string {
	return filepath.Join(c.ProfilesDir, name, externalDirName)
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

// isSymlink reports whether the given FileInfo represents a symlink.
func isSymlink(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
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

	// Create common/settings.json if missing
	commonSettings := filepath.Join(c.CommonDir, "settings.json")
	if _, err := os.Stat(commonSettings); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("checking %s: %w", commonSettings, err)
		}
		if err := os.WriteFile(commonSettings, []byte("{}\n"), 0o644); err != nil {
			return fmt.Errorf("creating %s: %w", commonSettings, err)
		}
	}

	// Create .gitignore to prevent accidental commit of OAuth tokens
	gitignorePath := filepath.Join(c.APMDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); errors.Is(err, os.ErrNotExist) {
		content := "# Prevent committing OAuth tokens and session state\n*/external/\nclaude-home-external/\n"
		if err := os.WriteFile(gitignorePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("creating %s: %w", gitignorePath, err)
		}
	}

	return nil
}
