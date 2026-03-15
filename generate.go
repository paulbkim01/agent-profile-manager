package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// cleanManagedItems removes only managed items and root-level symlinks from genDir.
// Runtime state (files/dirs created by Claude CLI) is preserved.
func cleanManagedItems(genDir string) error {
	// Remove items in managedItemSet
	for name := range managedItemSet {
		path := filepath.Join(genDir, name)
		if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing managed item %s: %w", name, err)
		}
	}

	// Remove root-level symlinks only (extras from common/profile).
	// Runtime-created regular files/dirs are preserved.
	entries, err := os.ReadDir(genDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading generated dir: %w", err)
	}
	for _, e := range entries {
		path := filepath.Join(genDir, e.Name())
		fi, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if isSymlink(fi) {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("removing symlink %s: %w", path, err)
			}
			log.Printf("generate: removed symlink %s", e.Name())
		}
	}

	return nil
}

// generateProfile builds the generated directory for a profile.
// Preserves runtime state (files created by Claude CLI) across rebuilds.
// Only managed items (settings, skills, commands, agents, rules, meta)
// and root-level symlinks are cleaned before regeneration.
func generateProfile(cfg *Config, name string) error {
	genDir := cfg.GeneratedProfileDir(name)
	profileDir := cfg.ProfileDir(name)

	// Verify profile exists
	if _, err := os.Stat(profileDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile '%s' not found", name)
		}
		return fmt.Errorf("checking profile directory: %w", err)
	}

	// Clean only managed items, preserving runtime state
	if err := cleanManagedItems(genDir); err != nil {
		return fmt.Errorf("cleaning managed items: %w", err)
	}
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return fmt.Errorf("creating generated dir: %w", err)
	}

	log.Printf("generate: building %s", genDir)

	// Step 1: Validate settings files before merging
	commonSettingsPath := filepath.Join(cfg.CommonDir, "settings.json")
	profileSettingsPath := filepath.Join(profileDir, "settings.json")
	if err := validateSettingsJSON(commonSettingsPath); err != nil {
		return fmt.Errorf("common settings: %w", err)
	}
	if err := validateSettingsJSON(profileSettingsPath); err != nil {
		return fmt.Errorf("profile settings: %w", err)
	}

	// Step 2: Deep merge settings.json
	if err := mergeProfileSettings(cfg, profileDir, genDir); err != nil {
		return fmt.Errorf("merging settings: %w", err)
	}

	// Step 3: Merge managed directories (skills/, commands/, agents/)
	for _, dir := range managedDirs {
		if err := mergeDir(cfg, profileDir, genDir, dir); err != nil {
			return fmt.Errorf("merging %s: %w", dir, err)
		}
	}

	// Step 4: Symlink extra files from common + profile (profile wins)
	if err := symlinkExtras(cfg.CommonDir, profileDir, genDir); err != nil {
		return fmt.Errorf("symlinking extras: %w", err)
	}

	// Step 5: Write meta hash for staleness detection
	if err := writeMetaHash(genDir); err != nil {
		return fmt.Errorf("writing meta hash: %w", err)
	}

	return nil
}

// mergeProfileSettings deep-merges common/settings.json + profile/settings.json.
func mergeProfileSettings(cfg *Config, profileDir, genDir string) error {
	commonSettings, err := loadJSON(filepath.Join(cfg.CommonDir, "settings.json"))
	if err != nil {
		return err
	}
	profileSettings, err := loadJSON(filepath.Join(profileDir, "settings.json"))
	if err != nil {
		return err
	}

	merged := mergeSettings(commonSettings, profileSettings)
	log.Printf("generate: merged settings (%d common keys + %d profile keys)", len(commonSettings), len(profileSettings))

	return writeJSON(filepath.Join(genDir, "settings.json"), merged)
}

// mergeDir creates a directory with symlinks from both common and profile sources.
// Profile entries override common entries with the same name.
func mergeDir(cfg *Config, profileDir, genDir, dirName string) error {
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

// symlinkExtras links any files/dirs from common and profile that aren't
// already handled by settings.json merge or managed dir merge.
// Profile entries override common entries with the same name.
// Nothing is linked from the backup — generated = common + profile only.
func symlinkExtras(commonDir, profileDir, genDir string) error {
	linked := make(map[string]bool)

	// Profile extras first (they win on conflict)
	if err := linkExtrasFrom(profileDir, genDir, "profile", linked); err != nil {
		return err
	}

	// Common extras (skip if profile already provided)
	if err := linkExtrasFrom(commonDir, genDir, "common", linked); err != nil {
		return err
	}

	return nil
}

// linkExtrasFrom symlinks files/dirs from srcDir into genDir, skipping
// managed items and anything already present in genDir.
func linkExtrasFrom(srcDir, genDir, label string, linked map[string]bool) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s dir: %w", label, err)
	}

	for _, e := range entries {
		name := e.Name()

		// Skip managed items (already handled)
		if managedItemSet[name] {
			continue
		}

		// Skip profile metadata and internal APM directories
		if name == "profile.yaml" || name == externalDirName {
			continue
		}

		// Skip if already linked by a higher-priority source
		if linked[name] {
			continue
		}

		dst := filepath.Join(genDir, name)

		// Skip if already exists in generated dir
		if _, err := os.Lstat(dst); err == nil {
			continue
		}

		src := filepath.Join(srcDir, name)
		realSrc, err := filepath.EvalSymlinks(src)
		if err != nil {
			realSrc = src
		}

		if err := os.Symlink(realSrc, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, realSrc, err)
		}
		log.Printf("generate: link %s -> %s", name, label)
		linked[name] = true
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
