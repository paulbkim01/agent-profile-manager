package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/paulbkim/agent-profile-manager/internal"
)

// ConfigFile is the YAML structure of config.yaml.
type ConfigFile struct {
	DefaultProfile string `yaml:"default_profile,omitempty"`
	ClaudeDir      string `yaml:"claude_dir,omitempty"`
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

// Load reads config.yaml and resolves all paths.
// apmDirOverride is from --config-dir flag (empty string = use default).
func Load(apmDirOverride string) (*Config, error) {
	apmDir := apmDirOverride
	if apmDir == "" {
		var err error
		apmDir, err = internal.DefaultAPMDir()
		if err != nil {
			return nil, fmt.Errorf("determining config directory: %w", err)
		}
	}

	claudeDir, err := internal.DefaultClaudeDir()
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

// SetDefaultProfile writes default_profile to config.yaml.
func (c *Config) SetDefaultProfile(name string) error {
	cf, err := c.readConfigFile()
	if err != nil {
		return err
	}

	cf.DefaultProfile = name

	out, err := yaml.Marshal(cf)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Atomic write: temp file + rename
	tmp := c.ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("writing temp config file: %w", err)
	}
	if err := os.Rename(tmp, c.ConfigPath); err != nil {
		os.Remove(tmp) // clean up orphaned temp file
		return fmt.Errorf("renaming temp config to %s: %w", c.ConfigPath, err)
	}
	return nil
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

// EnsureDirs creates the base directory structure if it doesn't exist.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.CommonDir,
		filepath.Join(c.CommonDir, "skills"),
		filepath.Join(c.CommonDir, "commands"),
		filepath.Join(c.CommonDir, "agents"),
		c.ProfilesDir,
		c.GeneratedDir,
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

	return nil
}
