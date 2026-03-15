package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// errSkipSymlink is returned by activateProfile when skipSymlink is true,
// signaling the caller to fall back to env-var-based activation.
var errSkipSymlink = errors.New("skip symlink activation")

// captureExternalState copies ~/.claude.json to targetDir/claude.json.
// Skips gracefully if ~/.claude.json doesn't exist.
func captureExternalState(targetDir string) error {
	src, err := claudeJSONPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("activate: no ~/.claude.json to capture")
			return nil
		}
		return fmt.Errorf("reading %s: %w", src, err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", targetDir, err)
	}
	dst := filepath.Join(targetDir, "claude.json")
	if err := atomicWriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	log.Printf("activate: captured external state to %s", dst)
	return nil
}

// restoreExternalState copies sourceDir/claude.json to ~/.claude.json.
// If sourceDir doesn't exist or has no claude.json (first-time profile use),
// ~/.claude.json is removed so Claude prompts for first-time setup.
func restoreExternalState(sourceDir string) error {
	dst, err := claudeJSONPath()
	if err != nil {
		return err
	}
	src := filepath.Join(sourceDir, "claude.json")
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("activate: no saved external state at %s, removing ~/.claude.json for fresh setup", sourceDir)
			if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("removing %s: %w", dst, err)
			}
			return nil
		}
		return fmt.Errorf("reading %s: %w", src, err)
	}
	if err := atomicWriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	log.Printf("activate: restored external state from %s", src)
	return nil
}

// restoreExternalStateTo copies sourceDir/claude.json to dstDir/claude.json.
// Used for copying external state between arbitrary directories (e.g. during
// profile overwrite). Unlike restoreExternalState, this does not remove the
// destination file when no source exists — it simply returns nil.
func restoreExternalStateTo(sourceDir, dstDir string) error {
	src := filepath.Join(sourceDir, "claude.json")
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", src, err)
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dstDir, err)
	}
	dst := filepath.Join(dstDir, "claude.json")
	if err := atomicWriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	log.Printf("activate: copied external state from %s to %s", sourceDir, dstDir)
	return nil
}

// activateProfile makes ~/.claude a symlink to the generated profile directory.
//
// If skipSymlink is true (dev mode / --config-dir override), it returns
// errSkipSymlink so the caller can fall back to env var output.
//
// On first activation, ~/.claude (if it exists as a real directory) is moved
// to APMDir/claude-home as a backup. Subsequent profile switches just
// re-point the symlink. Deactivation restores the backup.
func activateProfile(cfg *Config, name string, skipSymlink bool) error {
	if skipSymlink {
		return errSkipSymlink
	}

	claudePath, err := defaultClaudeDir()
	if err != nil {
		return err
	}

	backupPath := cfg.BackupDir()

	cf, err := cfg.readConfigFile()
	if err != nil {
		return err
	}
	alreadyBacked := cf.ClaudeHomePath != ""
	hasOverride := cf.ClaudeDir != ""

	fi, lstatErr := os.Lstat(claudePath)
	needsBackup := false

	switch {
	case lstatErr == nil && isSymlink(fi):
		// Switching profiles — capture old profile's external state before switch
		if cf.ActiveProfile != "" {
			if err := captureExternalState(cfg.ExternalStateDir(cf.ActiveProfile)); err != nil {
				return fmt.Errorf("capturing external state for '%s': %w", cf.ActiveProfile, err)
			}
		}

	case lstatErr == nil && fi.IsDir():
		if alreadyBacked || hasOverride {
			return fmt.Errorf(
				"%s is a directory but backup already exists at %s; resolve manually",
				claudePath, backupPath,
			)
		}
		needsBackup = true

	case errors.Is(lstatErr, os.ErrNotExist):
		if !alreadyBacked && !hasOverride {
			// No ~/.claude at all — create empty backup so ClaudeDir has a target
			log.Printf("activate: creating empty backup dir %s", backupPath)
			if err := os.MkdirAll(backupPath, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", backupPath, err)
			}
			cfg.ClaudeDir = backupPath
			cf.ClaudeHomePath = backupPath
			if err := cfg.writeConfigFile(cf); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}
		}

	default:
		return fmt.Errorf("checking %s: %w", claudePath, lstatErr)
	}

	// Backup real ~/.claude if needed (must happen before generation).
	if needsBackup {
		logInfo("Backing up ~/.claude to %s ...", backupPath)
		if err := os.Rename(claudePath, backupPath); err != nil {
			if errors.Is(err, syscall.EXDEV) {
				return fmt.Errorf(
					"%s and %s are on different filesystems; set claude_dir in config.yaml instead",
					claudePath, cfg.APMDir,
				)
			}
			return fmt.Errorf("backing up %s: %w", claudePath, err)
		}
		cfg.ClaudeDir = backupPath
		cf.ClaudeHomePath = backupPath
		if err := cfg.writeConfigFile(cf); err != nil {
			// Try to restore on failure
			os.Rename(backupPath, claudePath)
			return fmt.Errorf("saving config: %w", err)
		}
		logSuccess("Backed up ~/.claude (restore with: apm use --unset)")

		// Also capture external state to backup
		if err := captureExternalState(cfg.BackupExternalDir()); err != nil {
			return fmt.Errorf("capturing external state backup: %w", err)
		}
	}

	// Regenerate profile (cfg.ClaudeDir now points to backup or user override)
	if err := generateProfile(cfg, name); err != nil {
		return fmt.Errorf("generating profile: %w", err)
	}

	genDir := cfg.GeneratedProfileDir(name)

	// Remove old symlink if switching profiles (after successful generation)
	if lstatErr == nil && isSymlink(fi) {
		if err := os.Remove(claudePath); err != nil {
			return fmt.Errorf("removing symlink %s: %w", claudePath, err)
		}
	}

	// Create symlink: ~/.claude -> generated profile dir
	if err := os.Symlink(genDir, claudePath); err != nil {
		return fmt.Errorf("creating symlink %s -> %s: %w", claudePath, genDir, err)
	}
	log.Printf("activate: %s -> %s", claudePath, genDir)

	// Restore new profile's external state
	if err := restoreExternalState(cfg.ExternalStateDir(name)); err != nil {
		return fmt.Errorf("restoring external state for '%s': %w", name, err)
	}

	// Record active profile in config (reuse already-loaded cf)
	cf.ActiveProfile = name
	if err := cfg.writeConfigFile(cf); err != nil {
		return fmt.Errorf("saving active profile: %w", err)
	}

	return nil
}

// deactivateProfile removes the ~/.claude symlink and restores the backed-up
// real directory. Generated profile directories are preserved on disk for
// future reactivation. Use 'apm delete <name>' to permanently remove a
// profile and its generated dir.
func deactivateProfile(cfg *Config) error {
	claudePath, err := defaultClaudeDir()
	if err != nil {
		return err
	}

	cf, err := cfg.readConfigFile()
	if err != nil {
		return err
	}

	// Capture active profile's external state before deactivation
	if cf.ActiveProfile != "" {
		if err := captureExternalState(cfg.ExternalStateDir(cf.ActiveProfile)); err != nil {
			return fmt.Errorf("capturing external state for '%s': %w", cf.ActiveProfile, err)
		}
	}

	// Remove symlink if present
	fi, err := os.Lstat(claudePath)
	switch {
	case err == nil && isSymlink(fi):
		log.Printf("deactivate: removing symlink %s", claudePath)
		if err := os.Remove(claudePath); err != nil {
			return fmt.Errorf("removing symlink %s: %w", claudePath, err)
		}
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("checking %s: %w", claudePath, err)
	default:
		log.Printf("deactivate: %s is not a symlink, skipping removal", claudePath)
	}

	// Restore backup if we have one
	if cf.ClaudeHomePath != "" {
		if _, statErr := os.Stat(cf.ClaudeHomePath); statErr == nil {
			log.Printf("deactivate: restoring %s -> %s", cf.ClaudeHomePath, claudePath)
			if err := os.Rename(cf.ClaudeHomePath, claudePath); err != nil {
				return fmt.Errorf("restoring %s: %w", claudePath, err)
			}
		}
	}

	// Restore backup external state
	if err := restoreExternalState(cfg.BackupExternalDir()); err != nil {
		return fmt.Errorf("restoring external state backup: %w", err)
	}

	// Clear activation state
	return cfg.ClearActiveProfile()
}

// backupClaude copies the current ~/.claude state AND ~/.claude.json to the
// APM backup locations. Only works when no profile is active (i.e. ~/.claude
// is a real directory, not a symlink). The backup serves as the restore point
// when deactivating all profiles.
func backupClaude(cfg *Config) error {
	claudePath, err := defaultClaudeDir()
	if err != nil {
		return err
	}

	// Refuse to backup if a profile is active — ~/.claude would be a symlink
	// to a generated dir, not the real config
	fi, err := os.Lstat(claudePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s does not exist", claudePath)
		}
		return fmt.Errorf("checking %s: %w", claudePath, err)
	}
	if isSymlink(fi) {
		return fmt.Errorf("a profile is currently active. Deactivate first: apm use --unset")
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", claudePath)
	}

	backupPath := cfg.BackupDir()

	// Remove old backup
	if err := os.RemoveAll(backupPath); err != nil {
		return fmt.Errorf("removing old backup: %w", err)
	}

	// Copy directory tree
	if err := copyDir(claudePath, backupPath); err != nil {
		return fmt.Errorf("copying %s to %s: %w", claudePath, backupPath, err)
	}

	// Also capture external state (/.claude.json) to backup
	if err := captureExternalState(cfg.BackupExternalDir()); err != nil {
		return fmt.Errorf("capturing external state backup: %w", err)
	}

	// Record in config
	cf, err := cfg.readConfigFile()
	if err != nil {
		return err
	}
	cf.ClaudeHomePath = backupPath
	return cfg.writeConfigFile(cf)
}

// copyDir recursively copies src to dst, preserving symlinks.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// Use Lstat to detect symlinks
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		if isSymlink(info) {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
