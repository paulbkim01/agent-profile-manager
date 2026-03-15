package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ProfileMeta is the profile.yaml structure.
type ProfileMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	CreatedAt   string `yaml:"created_at"`
	Source      string `yaml:"source,omitempty"`
}

// ProfileInfo holds profile metadata plus its filesystem path.
type ProfileInfo struct {
	Meta ProfileMeta
	Dir  string
}

// createProfile makes a new profile directory with optional source import.
// source: "" = empty, "current" = import from ~/.claude, "<name>" = copy from profile.
func createProfile(cfg *Config, name, source, description string) error {
	if err := validateProfileName(name); err != nil {
		return err
	}

	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("profile '%s' already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking profile directory %s: %w", dir, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating profile directory %s: %w", dir, err)
	}

	// Create subdirs
	for _, sub := range managedDirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("creating subdirectory %s: %w", sub, err)
		}
	}

	// Handle source
	switch source {
	case "", "empty":
		// Write empty settings.json
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}\n"), 0o644); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("writing empty settings.json: %w", err)
		}
		log.Printf("profile: created empty profile '%s'", name)

	case "current":
		// Import from ~/.claude/
		if err := importFrom(cfg.ClaudeDir, dir); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("importing from current: %w", err)
		}
		log.Printf("profile: imported '%s' from %s", name, cfg.ClaudeDir)

	default:
		// Copy from another profile
		srcDir := cfg.ProfileDir(source)
		if _, err := os.Stat(srcDir); err != nil {
			os.RemoveAll(dir)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("source profile '%s' not found", source)
			}
			return fmt.Errorf("checking source profile directory %s: %w", srcDir, err)
		}
		if err := importFrom(srcDir, dir); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("copying from '%s': %w", source, err)
		}
		log.Printf("profile: copied '%s' from profile '%s'", name, source)
	}

	// After importing settings.json from source, validate it
	settingsDst := filepath.Join(dir, "settings.json")
	if err := validateSettingsJSON(settingsDst); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("imported settings invalid: %w", err)
	}

	// Write profile.yaml
	meta := ProfileMeta{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      source,
	}
	if err := writeMeta(dir, meta); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("writing profile metadata: %w", err)
	}
	return nil
}

// deleteProfile removes a profile. Checks if it's active first.
func deleteProfile(cfg *Config, name string, force bool) error {
	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile '%s' not found", name)
		}
		return fmt.Errorf("checking profile directory %s: %w", dir, err)
	}

	// Read default once for both the guard check and clearing
	defaultProfile, err := cfg.DefaultProfile()
	if err != nil {
		return fmt.Errorf("reading default profile: %w", err)
	}

	if !force {
		if defaultProfile == name {
			return fmt.Errorf("profile '%s' is the global default. Use --force to delete anyway", name)
		}
		if activeProfile := os.Getenv("APM_PROFILE"); activeProfile == name {
			return fmt.Errorf("profile '%s' is active in this shell. Use --force to delete anyway", name)
		}
	}

	// Clear default if this was it
	if defaultProfile == name {
		if err := cfg.ClearDefaultProfile(); err != nil {
			return fmt.Errorf("clearing default profile: %w", err)
		}
		log.Printf("profile: cleared global default (was '%s')", name)
	}

	// Remove generated dir
	genDir := cfg.GeneratedProfileDir(name)
	if err := os.RemoveAll(genDir); err != nil {
		return fmt.Errorf("removing generated directory for '%s': %w", name, err)
	}

	// Remove profile dir
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("deleting '%s': %w", name, err)
	}

	log.Printf("profile: deleted '%s'", name)
	return nil
}

// listProfiles returns all profiles.
func listProfiles(cfg *Config) ([]ProfileInfo, error) {
	entries, err := os.ReadDir(cfg.ProfilesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading profiles directory: %w", err)
	}

	var profiles []ProfileInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := readMeta(filepath.Join(cfg.ProfilesDir, e.Name()))
		if err != nil {
			// Skip directories without profile.yaml
			log.Printf("profile: skipping %s: %v", e.Name(), err)
			continue
		}
		profiles = append(profiles, ProfileInfo{
			Meta: meta,
			Dir:  filepath.Join(cfg.ProfilesDir, e.Name()),
		})
	}
	return profiles, nil
}

// getProfile returns a single profile's info.
func getProfile(cfg *Config, name string) (ProfileInfo, error) {
	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProfileInfo{}, fmt.Errorf("profile '%s' not found", name)
		}
		return ProfileInfo{}, fmt.Errorf("checking profile directory %s: %w", dir, err)
	}
	meta, err := readMeta(dir)
	if err != nil {
		return ProfileInfo{}, fmt.Errorf("reading profile '%s' metadata: %w", name, err)
	}
	return ProfileInfo{Meta: meta, Dir: dir}, nil
}

// profileExists checks if a profile exists.
// Returns false for both missing profiles and unexpected stat errors.
// Callers that need to distinguish should use getProfile() instead.
func profileExists(cfg *Config, name string) bool {
	_, err := os.Stat(cfg.ProfileDir(name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("profile: unexpected error checking %s: %v", name, err)
	}
	return err == nil
}

// importFrom copies settings.json and managed dirs from src to dst.
func importFrom(src, dst string) error {
	// Copy settings.json
	settingsSrc := filepath.Join(src, "settings.json")
	settingsDst := filepath.Join(dst, "settings.json")
	if data, err := os.ReadFile(settingsSrc); err == nil {
		if err := os.WriteFile(settingsDst, data, 0o644); err != nil {
			return fmt.Errorf("writing settings.json: %w", err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// No settings.json in source, write empty
		if err := os.WriteFile(settingsDst, []byte("{}\n"), 0o644); err != nil {
			return fmt.Errorf("writing empty settings.json: %w", err)
		}
	} else {
		return fmt.Errorf("reading source settings.json: %w", err)
	}

	// Copy managed dirs using shared copyDir
	for _, dir := range managedDirs {
		srcDir := filepath.Join(src, dir)
		dstDir := filepath.Join(dst, dir)
		if _, err := os.Stat(srcDir); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := copyDir(srcDir, dstDir); err != nil {
			return fmt.Errorf("copying %s directory: %w", dir, err)
		}
	}
	return nil
}

func writeMeta(dir string, meta ProfileMeta) error {
	data, err := yaml.Marshal(&meta)
	if err != nil {
		return fmt.Errorf("marshaling profile metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), data, 0o644); err != nil {
		return fmt.Errorf("writing profile.yaml: %w", err)
	}
	return nil
}

func readMeta(dir string) (ProfileMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "profile.yaml"))
	if err != nil {
		return ProfileMeta{}, fmt.Errorf("reading profile.yaml in %s: %w", dir, err)
	}
	var meta ProfileMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return ProfileMeta{}, fmt.Errorf("parsing profile.yaml in %s: %w", dir, err)
	}
	return meta, nil
}
