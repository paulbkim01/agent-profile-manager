# Task 1: Generation engine

## Files to create
- `internal/generate/generate.go`
- `internal/generate/generate_test.go`

## Dependencies
- Phase 2 (merge engine)
- Phase 3 (profile CRUD -- for reading profile data)

## Implementation

### internal/generate/generate.go

The generator builds a complete `CLAUDE_CONFIG_DIR`-compatible directory by:
1. Deep-merging common + profile settings.json
2. Creating merged skill/command/agent directories with symlinks from both sources
3. Symlinking everything else from `~/.claude/`

```go
package generate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/paulbkim/agent-profile-manager/internal"
	"github.com/paulbkim/agent-profile-manager/internal/config"
	"github.com/paulbkim/agent-profile-manager/internal/merge"
	"github.com/paulbkim/agent-profile-manager/internal/validate"
)

// Profile builds the generated directory for a profile.
// Cleans and rebuilds from scratch each time.
func Profile(cfg *config.Config, name string) error {
	genDir := cfg.GeneratedProfileDir(name)
	profileDir := cfg.ProfileDir(name)

	// Verify profile exists
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		return fmt.Errorf("profile '%s' not found", name)
	}

	// Clean previous generated dir
	os.RemoveAll(genDir)
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return fmt.Errorf("creating generated dir: %w", err)
	}

	log.Printf("generate: building %s", genDir)

	// Step 1: Validate settings files before merging
	commonSettingsPath := filepath.Join(cfg.CommonDir, "settings.json")
	profileSettingsPath := filepath.Join(profileDir, "settings.json")
	if err := validate.SettingsJSON(commonSettingsPath); err != nil {
		return fmt.Errorf("common settings: %w", err)
	}
	if err := validate.SettingsJSON(profileSettingsPath); err != nil {
		return fmt.Errorf("profile settings: %w", err)
	}

	// Step 2: Deep merge settings.json
	if err := mergeSettings(cfg, profileDir, genDir); err != nil {
		return fmt.Errorf("merging settings: %w", err)
	}

	// Step 3: Merge managed directories (skills/, commands/, agents/)
	for _, dir := range internal.ManagedDirs {
		if err := mergeDir(cfg, profileDir, genDir, dir); err != nil {
			return fmt.Errorf("merging %s: %w", dir, err)
		}
	}

	// Step 4: Symlink everything else from ~/.claude/
	if err := symlinkShared(cfg, genDir); err != nil {
		return fmt.Errorf("symlinking shared: %w", err)
	}

	// Step 5: Write meta hash for staleness detection
	return writeMetaHash(genDir)
}

// mergeSettings deep-merges common/settings.json + profile/settings.json.
func mergeSettings(cfg *config.Config, profileDir, genDir string) error {
	commonSettings, err := merge.LoadJSON(filepath.Join(cfg.CommonDir, "settings.json"))
	if err != nil {
		return err
	}
	profileSettings, err := merge.LoadJSON(filepath.Join(profileDir, "settings.json"))
	if err != nil {
		return err
	}

	merged := merge.Settings(commonSettings, profileSettings)
	log.Printf("generate: merged settings (%d common keys + %d profile keys)", len(commonSettings), len(profileSettings))

	return merge.WriteJSON(filepath.Join(genDir, "settings.json"), merged)
}

// mergeDir creates a directory with symlinks from both common and profile sources.
// Profile entries override common entries with the same name.
func mergeDir(cfg *config.Config, profileDir, genDir, dirName string) error {
	targetDir := filepath.Join(genDir, dirName)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	commonSrc := filepath.Join(cfg.CommonDir, dirName)
	profileSrc := filepath.Join(profileDir, dirName)

	linked := make(map[string]bool)

	// Profile entries first (they win on conflict)
	if entries, err := os.ReadDir(profileSrc); err == nil {
		for _, e := range entries {
			src := filepath.Join(profileSrc, e.Name())
			dst := filepath.Join(targetDir, e.Name())

			// Resolve the actual target if src is itself a symlink
			realSrc, err := filepath.EvalSymlinks(src)
			if err != nil {
				realSrc = src
			}

			if err := os.Symlink(realSrc, dst); err != nil {
				log.Printf("generate: warn: symlink %s -> %s: %v", dst, realSrc, err)
			} else {
				log.Printf("generate: link %s/%s -> profile", dirName, e.Name())
			}
			linked[e.Name()] = true
		}
	}

	// Common entries (skip if profile already has same name)
	if entries, err := os.ReadDir(commonSrc); err == nil {
		for _, e := range entries {
			if linked[e.Name()] {
				log.Printf("generate: skip %s/%s (overridden by profile)", dirName, e.Name())
				continue
			}
			src := filepath.Join(commonSrc, e.Name())
			dst := filepath.Join(targetDir, e.Name())

			realSrc, err := filepath.EvalSymlinks(src)
			if err != nil {
				realSrc = src
			}

			if err := os.Symlink(realSrc, dst); err != nil {
				log.Printf("generate: warn: symlink %s -> %s: %v", dst, realSrc, err)
			} else {
				log.Printf("generate: link %s/%s -> common", dirName, e.Name())
			}
		}
	}

	return nil
}

// symlinkShared creates symlinks for everything in ~/.claude/ that isn't
// in ManagedItems (those are handled by mergeSettings/mergeDir).
func symlinkShared(cfg *config.Config, genDir string) error {
	managed := make(map[string]bool)
	for _, item := range internal.ManagedItems {
		managed[item] = true
	}

	entries, err := os.ReadDir(cfg.ClaudeDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("generate: ~/.claude/ not found, skipping shared symlinks")
			return nil
		}
		return err
	}

	for _, e := range entries {
		name := e.Name()

		// Skip managed items (we handle those separately)
		if managed[name] {
			continue
		}

		// Skip backup files like settings.json.work
		if filepath.Ext(name) == ".work" || filepath.Ext(name) == ".backup" {
			continue
		}

		src := filepath.Join(cfg.ClaudeDir, name)
		dst := filepath.Join(genDir, name)

		// Skip if already exists in generated dir
		if _, err := os.Lstat(dst); err == nil {
			continue
		}

		if err := os.Symlink(src, dst); err != nil {
			log.Printf("generate: warn: symlink %s: %v", name, err)
		} else {
			log.Printf("generate: link %s -> shared", name)
		}
	}

	return nil
}

// writeMetaHash writes a .apm-meta.json with a content hash for staleness detection.
func writeMetaHash(genDir string) error {
	settingsPath := filepath.Join(genDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil // not critical
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	meta := map[string]string{"settings_hash": hash}
	out, _ := json.Marshal(meta)
	return os.WriteFile(filepath.Join(genDir, ".apm-meta.json"), out, 0o644)
}
```

### internal/generate/generate_test.go

```go
package generate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/paulbkim/agent-profile-manager/internal/config"
)

func setup(t *testing.T) *config.Config {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.Config{
		APMDir:       filepath.Join(tmp, "apm"),
		ClaudeDir:    filepath.Join(tmp, ".claude"),
		CommonDir:    filepath.Join(tmp, "apm", "common"),
		ProfilesDir:  filepath.Join(tmp, "apm", "profiles"),
		GeneratedDir: filepath.Join(tmp, "apm", "generated"),
		ConfigPath:   filepath.Join(tmp, "apm", "config.yaml"),
	}
	cfg.EnsureDirs()

	// Mock ~/.claude/
	claude := cfg.ClaudeDir
	os.MkdirAll(claude, 0o755)
	os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"effortLevel":"high"}`), 0o644)
	os.WriteFile(filepath.Join(claude, "history.jsonl"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(claude, "settings.local.json"), []byte(`{}`), 0o644)
	os.MkdirAll(filepath.Join(claude, "skills"), 0o755)
	os.MkdirAll(filepath.Join(claude, "plugins"), 0o755)
	os.MkdirAll(filepath.Join(claude, "sessions"), 0o755)
	os.MkdirAll(filepath.Join(claude, "projects"), 0o755)

	// Common settings
	os.WriteFile(filepath.Join(cfg.CommonDir, "settings.json"),
		[]byte(`{"permissions":{"allow":["Read","Write"]}}`), 0o644)

	// Profile
	profDir := filepath.Join(cfg.ProfilesDir, "work")
	os.MkdirAll(profDir, 0o755)
	os.MkdirAll(filepath.Join(profDir, "skills"), 0o755)
	os.MkdirAll(filepath.Join(profDir, "commands"), 0o755)
	os.MkdirAll(filepath.Join(profDir, "agents"), 0o755)
	os.WriteFile(filepath.Join(profDir, "settings.json"),
		[]byte(`{"model":"opus","permissions":{"allow":["Grep"]}}`), 0o644)
	os.WriteFile(filepath.Join(profDir, "profile.yaml"),
		[]byte("name: work\n"), 0o644)

	return cfg
}

func TestGenerate(t *testing.T) {
	cfg := setup(t)

	if err := Profile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")

	// Check merged settings.json exists
	data, err := os.ReadFile(filepath.Join(genDir, "settings.json"))
	if err != nil {
		t.Fatal("settings.json not found in generated dir")
	}
	// Should contain model from profile
	if len(data) == 0 {
		t.Error("settings.json is empty")
	}

	// Check shared items are symlinked
	for _, shared := range []string{"history.jsonl", "settings.local.json", "plugins", "sessions", "projects"} {
		link := filepath.Join(genDir, shared)
		fi, err := os.Lstat(link)
		if err != nil {
			t.Errorf("expected symlink for %s: %v", shared, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("expected %s to be a symlink", shared)
		}
	}

	// Check managed dirs exist as real directories (not symlinks)
	for _, dir := range []string{"skills", "commands", "agents"} {
		fi, err := os.Lstat(filepath.Join(genDir, dir))
		if err != nil {
			t.Errorf("expected %s dir: %v", dir, err)
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s should be a real directory, not a symlink", dir)
		}
	}

	// Check .apm-meta.json exists
	if _, err := os.Stat(filepath.Join(genDir, ".apm-meta.json")); err != nil {
		t.Error("expected .apm-meta.json")
	}
}

func TestGenerateRebuilds(t *testing.T) {
	cfg := setup(t)

	// Generate twice -- should clean and rebuild
	Profile(cfg, "work")
	Profile(cfg, "work")

	genDir := cfg.GeneratedProfileDir("work")
	if _, err := os.Stat(filepath.Join(genDir, "settings.json")); err != nil {
		t.Error("settings.json missing after rebuild")
	}
}

func TestGenerateMissingClaude(t *testing.T) {
	cfg := setup(t)
	os.RemoveAll(cfg.ClaudeDir)

	// Should not fail, just skip symlinks
	if err := Profile(cfg, "work"); err != nil {
		t.Fatal(err)
	}
}
```

## Verification

```bash
go test -v ./internal/generate/
```
