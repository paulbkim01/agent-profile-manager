package generate

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	if _, err := os.Stat(profileDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile '%s' not found", name)
		}
		return fmt.Errorf("checking profile directory: %w", err)
	}

	// Clean previous generated dir
	if err := os.RemoveAll(genDir); err != nil {
		return fmt.Errorf("removing previous generated dir: %w", err)
	}
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
	if err := writeMetaHash(genDir); err != nil {
		return fmt.Errorf("writing meta hash: %w", err)
	}

	return nil
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
		return fmt.Errorf("creating target dir %s: %w", targetDir, err)
	}

	commonSrc := filepath.Join(cfg.CommonDir, dirName)
	profileSrc := filepath.Join(profileDir, dirName)

	linked := make(map[string]bool)

	// Profile entries first (they win on conflict)
	if err := symlinkEntries(profileSrc, targetDir, dirName, "profile", linked); err != nil {
		return err
	}

	// Common entries (skip if profile already has same name)
	if err := symlinkEntries(commonSrc, targetDir, dirName, "common", linked); err != nil {
		return err
	}

	return nil
}

// symlinkEntries reads entries from srcDir and creates symlinks in targetDir.
// Already-linked names in the linked map are skipped (profile wins over common).
// New entries are recorded in linked. label is used for log messages.
func symlinkEntries(srcDir, targetDir, dirName, label string, linked map[string]bool) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s dir %s: %w", label, srcDir, err)
	}
	for _, e := range entries {
		if linked[e.Name()] {
			log.Printf("generate: skip %s/%s (overridden by profile)", dirName, e.Name())
			continue
		}

		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(targetDir, e.Name())

		// Resolve the actual target if src is itself a symlink
		realSrc, err := filepath.EvalSymlinks(src)
		if err != nil {
			realSrc = src
		}

		if err := os.Symlink(realSrc, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, realSrc, err)
		}
		log.Printf("generate: link %s/%s -> %s", dirName, e.Name(), label)
		linked[e.Name()] = true
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
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("generate: %s not found, skipping shared symlinks", cfg.ClaudeDir)
			return nil
		}
		return fmt.Errorf("reading claude dir %s: %w", cfg.ClaudeDir, err)
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
			return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
		log.Printf("generate: link %s -> shared", name)
	}

	return nil
}

// writeMetaHash writes a .apm-meta.json with a content hash for staleness detection.
func writeMetaHash(genDir string) error {
	settingsPath := filepath.Join(genDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		// Not critical if settings.json is missing, but log it
		log.Printf("generate: warn: cannot read settings.json for hash: %v", err)
		return nil
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	meta := map[string]string{"settings_hash": hash}
	out, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshaling meta hash: %w", err)
	}
	if err := os.WriteFile(filepath.Join(genDir, ".apm-meta.json"), out, 0o644); err != nil {
		return fmt.Errorf("writing .apm-meta.json: %w", err)
	}
	return nil
}
