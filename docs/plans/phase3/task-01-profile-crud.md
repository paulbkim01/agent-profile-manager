# Task 1: Profile CRUD

## Files to create
- `internal/profile/profile.go`
- `internal/profile/profile_test.go`

## Dependencies
- Phase 1 (config, constants, validate)

## Implementation

### internal/profile/profile.go

```go
package profile

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/paulbkim/agent-profile-manager/internal"
	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/validate"
)

// Meta is the profile.yaml structure.
type Meta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	CreatedAt   string `yaml:"created_at"`
	Source      string `yaml:"source,omitempty"`
}

// Info holds profile metadata plus its filesystem path.
type Info struct {
	Meta Meta
	Dir  string
}

// Create makes a new profile directory with optional source import.
// source: "" = empty, "current" = import from ~/.claude, "<name>" = copy from profile.
func Create(cfg *config.Config, name, source, description string) error {
	if err := validate.ProfileName(name); err != nil {
		return err
	}

	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("profile '%s' already exists", name)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Create subdirs
	for _, sub := range internal.ManagedDirs {
		os.MkdirAll(filepath.Join(dir, sub), 0o755)
	}

	// Handle source
	switch source {
	case "", "empty":
		// Write empty settings.json
		os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}\n"), 0o644)
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
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			os.RemoveAll(dir)
			return fmt.Errorf("source profile '%s' not found", source)
		}
		if err := importFrom(srcDir, dir); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("copying from '%s': %w", source, err)
		}
		log.Printf("profile: copied '%s' from profile '%s'", name, source)
	}

	// After importing settings.json from source, validate it
	settingsDst := filepath.Join(dir, "settings.json")
	if err := validate.SettingsJSON(settingsDst); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("imported settings invalid: %w", err)
	}

	// Write profile.yaml
	meta := Meta{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      source,
	}
	return writeMeta(dir, meta)
}

// Delete removes a profile. Checks if it's active first.
func Delete(cfg *config.Config, name string, force bool) error {
	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("profile '%s' not found", name)
	}

	// Check if active
	if !force {
		if cfg.DefaultProfile() == name {
			return fmt.Errorf("profile '%s' is the global default. Use --force to delete anyway", name)
		}
		if activeProfile := os.Getenv("APM_PROFILE"); activeProfile == name {
			return fmt.Errorf("profile '%s' is active in this shell. Use --force to delete anyway", name)
		}
	}

	// Clear default if this was it
	if cfg.DefaultProfile() == name {
		cfg.ClearDefaultProfile()
		log.Printf("profile: cleared global default (was '%s')", name)
	}

	// Remove generated dir
	genDir := cfg.GeneratedProfileDir(name)
	os.RemoveAll(genDir)

	// Remove profile dir
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("deleting '%s': %w", name, err)
	}

	log.Printf("profile: deleted '%s'", name)
	return nil
}

// List returns all profiles.
func List(cfg *config.Config) ([]Info, error) {
	entries, err := os.ReadDir(cfg.ProfilesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var profiles []Info
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := readMeta(filepath.Join(cfg.ProfilesDir, e.Name()))
		if err != nil {
			// Skip directories without profile.yaml
			continue
		}
		profiles = append(profiles, Info{
			Meta: meta,
			Dir:  filepath.Join(cfg.ProfilesDir, e.Name()),
		})
	}
	return profiles, nil
}

// Get returns a single profile's info.
func Get(cfg *config.Config, name string) (Info, error) {
	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return Info{}, fmt.Errorf("profile '%s' not found", name)
	}
	meta, err := readMeta(dir)
	if err != nil {
		return Info{}, err
	}
	return Info{Meta: meta, Dir: dir}, nil
}

// Exists checks if a profile exists.
func Exists(cfg *config.Config, name string) bool {
	_, err := os.Stat(cfg.ProfileDir(name))
	return err == nil
}

// importFrom copies settings.json and managed dirs from src to dst.
func importFrom(src, dst string) error {
	// Copy settings.json
	settingsSrc := filepath.Join(src, "settings.json")
	settingsDst := filepath.Join(dst, "settings.json")
	if data, err := os.ReadFile(settingsSrc); err == nil {
		if err := os.WriteFile(settingsDst, data, 0o644); err != nil {
			return err
		}
	} else {
		// No settings.json in source, write empty
		os.WriteFile(settingsDst, []byte("{}\n"), 0o644)
	}

	// Copy managed dirs
	for _, dir := range internal.ManagedDirs {
		srcDir := filepath.Join(src, dir)
		dstDir := filepath.Join(dst, dir)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}
		// Walk and copy (preserving symlinks)
		filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(srcDir, path)
			if rel == "." {
				return nil
			}
			target := filepath.Join(dstDir, rel)

			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}

			// Check if it's a symlink
			info, err := os.Lstat(path)
			if err != nil {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				link, err := os.Readlink(path)
				if err != nil {
					return nil
				}
				return os.Symlink(link, target)
			}

			// Regular file: copy
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		})
	}
	return nil
}

func writeMeta(dir string, meta Meta) error {
	data, err := yaml.Marshal(&meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "profile.yaml"), data, 0o644)
}

func readMeta(dir string) (Meta, error) {
	data, err := os.ReadFile(filepath.Join(dir, "profile.yaml"))
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return Meta{}, err
	}
	return meta, nil
}
```

### internal/profile/profile_test.go

```go
package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/paulbkim/agent-profile-manager/internal/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		APMDir:       filepath.Join(tmp, ".config", "apm"),
		ClaudeDir:    filepath.Join(tmp, ".claude"),
		CommonDir:    filepath.Join(tmp, ".config", "apm", "common"),
		ProfilesDir:  filepath.Join(tmp, ".config", "apm", "profiles"),
		GeneratedDir: filepath.Join(tmp, ".config", "apm", "generated"),
		ConfigPath:   filepath.Join(tmp, ".config", "apm", "config.yaml"),
	}
	cfg.EnsureDirs()

	// Create mock ~/.claude/
	os.MkdirAll(cfg.ClaudeDir, 0o755)
	os.WriteFile(filepath.Join(cfg.ClaudeDir, "settings.json"),
		[]byte(`{"effortLevel":"high"}`), 0o644)
	os.MkdirAll(filepath.Join(cfg.ClaudeDir, "skills"), 0o755)
	os.MkdirAll(filepath.Join(cfg.ClaudeDir, "commands"), 0o755)
	os.MkdirAll(filepath.Join(cfg.ClaudeDir, "agents"), 0o755)
	return cfg
}

func TestCreateEmpty(t *testing.T) {
	cfg := testConfig(t)
	if err := Create(cfg, "personal", "", "my personal profile"); err != nil {
		t.Fatal(err)
	}
	if !Exists(cfg, "personal") {
		t.Error("profile should exist")
	}
	info, _ := Get(cfg, "personal")
	if info.Meta.Name != "personal" {
		t.Errorf("name: got %s", info.Meta.Name)
	}
	if info.Meta.Description != "my personal profile" {
		t.Errorf("description: got %s", info.Meta.Description)
	}
}

func TestCreateFromCurrent(t *testing.T) {
	cfg := testConfig(t)
	if err := Create(cfg, "work", "current", ""); err != nil {
		t.Fatal(err)
	}
	// Should have copied settings.json from ~/.claude/
	data, err := os.ReadFile(filepath.Join(cfg.ProfileDir("work"), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"effortLevel":"high"}` {
		t.Errorf("settings: got %s", string(data))
	}
}

func TestCreateDuplicate(t *testing.T) {
	cfg := testConfig(t)
	Create(cfg, "work", "", "")
	if err := Create(cfg, "work", "", ""); err == nil {
		t.Error("expected error for duplicate")
	}
}

func TestCreateInvalidName(t *testing.T) {
	cfg := testConfig(t)
	if err := Create(cfg, "common", "", ""); err == nil {
		t.Error("expected error for reserved name")
	}
	if err := Create(cfg, "My Profile", "", ""); err == nil {
		t.Error("expected error for invalid name")
	}
}

func TestDelete(t *testing.T) {
	cfg := testConfig(t)
	Create(cfg, "temp", "", "")
	if err := Delete(cfg, "temp", false); err != nil {
		t.Fatal(err)
	}
	if Exists(cfg, "temp") {
		t.Error("profile should be deleted")
	}
}

func TestDeleteActiveRefused(t *testing.T) {
	cfg := testConfig(t)
	Create(cfg, "active", "", "")
	cfg.SetDefaultProfile("active")
	if err := Delete(cfg, "active", false); err == nil {
		t.Error("expected error deleting active profile without --force")
	}
}

func TestDeleteActiveForced(t *testing.T) {
	cfg := testConfig(t)
	Create(cfg, "active", "", "")
	cfg.SetDefaultProfile("active")
	if err := Delete(cfg, "active", true); err != nil {
		t.Fatal(err)
	}
	if Exists(cfg, "active") {
		t.Error("profile should be deleted")
	}
	if cfg.DefaultProfile() != "" {
		t.Error("default should be cleared")
	}
}

func TestList(t *testing.T) {
	cfg := testConfig(t)
	Create(cfg, "alpha", "", "first")
	Create(cfg, "beta", "", "second")
	profiles, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(profiles))
	}
}
```

## Verification

```bash
go test -v ./internal/profile/
```
