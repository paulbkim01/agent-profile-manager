package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupGenerateTest(t *testing.T) *Config {
	t.Helper()
	cfg := newTestConfig(t)

	// Mock ~/.claude/ (backup — should NOT leak into generated dirs)
	claude := cfg.ClaudeDir
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatalf("creating claude dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(`{"effortLevel":"high"}`), 0o644); err != nil {
		t.Fatalf("writing claude settings.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claude, "history.jsonl"), []byte(""), 0o644); err != nil {
		t.Fatalf("writing history.jsonl: %v", err)
	}

	// Common settings
	if err := os.WriteFile(filepath.Join(cfg.CommonDir, "settings.json"),
		[]byte(`{"permissions":{"allow":["Read","Write"]}}`), 0o644); err != nil {
		t.Fatalf("writing common settings.json: %v", err)
	}

	// Profile
	profDir := filepath.Join(cfg.ProfilesDir, "work")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatalf("creating profile dir: %v", err)
	}
	for _, sub := range managedDirs {
		if err := os.MkdirAll(filepath.Join(profDir, sub), 0o755); err != nil {
			t.Fatalf("creating profile %s dir: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(profDir, "settings.json"),
		[]byte(`{"model":"opus","permissions":{"allow":["Grep"]}}`), 0o644); err != nil {
		t.Fatalf("writing profile settings.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "profile.yaml"),
		[]byte("name: work\n"), 0o644); err != nil {
		t.Fatalf("writing profile.yaml: %v", err)
	}

	return cfg
}

func TestGenerate(t *testing.T) {
	cfg := setupGenerateTest(t)

	if err := generateProfile(cfg, "work"); err != nil {
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

	// Verify deep merge: should have model from profile and permissions union
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing merged settings.json: %v", err)
	}
	if settings["model"] != "opus" {
		t.Errorf("expected model=opus, got %v", settings["model"])
	}
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatal("expected permissions to be an object")
	}
	allow, ok := perms["allow"].([]any)
	if !ok {
		t.Fatal("expected permissions.allow to be an array")
	}
	// Should be union of common (Read, Write) and profile (Grep)
	if len(allow) != 3 {
		t.Errorf("expected 3 permissions, got %d: %v", len(allow), allow)
	}

	// Backup items should NOT appear in generated dir
	for _, backupOnly := range []string{"history.jsonl"} {
		if _, err := os.Lstat(filepath.Join(genDir, backupOnly)); err == nil {
			t.Errorf("%s should not be in generated dir (backup only)", backupOnly)
		}
	}

	// Check managed dirs exist as real directories (not symlinks)
	for _, dir := range managedDirs {
		fi, err := os.Lstat(filepath.Join(genDir, dir))
		if err != nil {
			t.Errorf("expected %s dir: %v", dir, err)
			continue
		}
		if isSymlink(fi) {
			t.Errorf("%s should be a real directory, not a symlink", dir)
		}
	}

	// Check .apm-meta.json exists
	if _, err := os.Stat(filepath.Join(genDir, ".apm-meta.json")); err != nil {
		t.Error("expected .apm-meta.json")
	}
}

func TestGenerateRebuilds(t *testing.T) {
	cfg := setupGenerateTest(t)

	// Generate twice -- should clean and rebuild
	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatalf("second generate: %v", err)
	}

	genDir := cfg.GeneratedProfileDir("work")
	if _, err := os.Stat(filepath.Join(genDir, "settings.json")); err != nil {
		t.Error("settings.json missing after rebuild")
	}
}

func TestGenerateMissingClaude(t *testing.T) {
	cfg := setupGenerateTest(t)
	if err := os.RemoveAll(cfg.ClaudeDir); err != nil {
		t.Fatalf("removing claude dir: %v", err)
	}

	// Should not fail, just skip symlinks
	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	// Verify settings.json was still generated
	genDir := cfg.GeneratedProfileDir("work")
	if _, err := os.Stat(filepath.Join(genDir, "settings.json")); err != nil {
		t.Error("settings.json missing even though merge should still work")
	}
}

func TestGenerateMissingProfile(t *testing.T) {
	cfg := setupGenerateTest(t)

	err := generateProfile(cfg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
}

func TestGenerateMergeDirOverride(t *testing.T) {
	cfg := setupGenerateTest(t)

	// Add a skill to common
	commonSkill := filepath.Join(cfg.CommonDir, "skills", "shared-skill.md")
	if err := os.WriteFile(commonSkill, []byte("shared skill"), 0o644); err != nil {
		t.Fatalf("writing common skill: %v", err)
	}

	// Add a skill with the same name to profile (should override)
	profSkill := filepath.Join(cfg.ProfileDir("work"), "skills", "shared-skill.md")
	if err := os.WriteFile(profSkill, []byte("profile skill"), 0o644); err != nil {
		t.Fatalf("writing profile skill: %v", err)
	}

	// Add a common-only skill
	commonOnlySkill := filepath.Join(cfg.CommonDir, "skills", "common-only.md")
	if err := os.WriteFile(commonOnlySkill, []byte("common only"), 0o644); err != nil {
		t.Fatalf("writing common-only skill: %v", err)
	}

	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")
	skillsDir := filepath.Join(genDir, "skills")

	// shared-skill.md should be a symlink to profile version
	link, err := os.Readlink(filepath.Join(skillsDir, "shared-skill.md"))
	if err != nil {
		t.Fatalf("reading symlink for shared-skill.md: %v", err)
	}
	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("reading linked file: %v", err)
	}
	if string(data) != "profile skill" {
		t.Errorf("expected profile version of shared-skill.md, got %q", string(data))
	}

	// common-only.md should be a symlink to common version
	link, err = os.Readlink(filepath.Join(skillsDir, "common-only.md"))
	if err != nil {
		t.Fatalf("reading symlink for common-only.md: %v", err)
	}
	data, err = os.ReadFile(link)
	if err != nil {
		t.Fatalf("reading linked file: %v", err)
	}
	if string(data) != "common only" {
		t.Errorf("expected common version of common-only.md, got %q", string(data))
	}
}

func TestGenerateBackupDoesNotLeak(t *testing.T) {
	cfg := setupGenerateTest(t)

	// Add extra files to backup (claude dir) — none should leak
	for _, name := range []string{"credentials.json", "cost_history.json", "sessions"} {
		path := filepath.Join(cfg.ClaudeDir, name)
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")

	for _, name := range []string{"credentials.json", "cost_history.json", "sessions", "history.jsonl"} {
		if _, err := os.Lstat(filepath.Join(genDir, name)); err == nil {
			t.Errorf("%s should not be in generated dir (backup does not leak)", name)
		}
	}
}

func TestGenerateExtrasFromCommonAndProfile(t *testing.T) {
	cfg := setupGenerateTest(t)

	// Add an extra file to common
	if err := os.WriteFile(filepath.Join(cfg.CommonDir, "CLAUDE.md"), []byte("# shared"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add an extra file to profile
	profDir := cfg.ProfileDir("work")
	if err := os.WriteFile(filepath.Join(profDir, "credentials.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")

	// Common extra should be symlinked
	fi, err := os.Lstat(filepath.Join(genDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("expected CLAUDE.md from common: %v", err)
	}
	if !isSymlink(fi) {
		t.Error("CLAUDE.md should be a symlink")
	}

	// Profile extra should be symlinked
	fi, err = os.Lstat(filepath.Join(genDir, "credentials.json"))
	if err != nil {
		t.Fatalf("expected credentials.json from profile: %v", err)
	}
	if !isSymlink(fi) {
		t.Error("credentials.json should be a symlink")
	}
}

func TestGenerateProfileExtrasOverrideCommon(t *testing.T) {
	cfg := setupGenerateTest(t)

	// Same file in both common and profile — profile wins
	if err := os.WriteFile(filepath.Join(cfg.CommonDir, "CLAUDE.md"), []byte("common"), 0o644); err != nil {
		t.Fatal(err)
	}
	profDir := cfg.ProfileDir("work")
	if err := os.WriteFile(filepath.Join(profDir, "CLAUDE.md"), []byte("profile"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")
	target, err := os.Readlink(filepath.Join(genDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("readlink CLAUDE.md: %v", err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "profile" {
		t.Errorf("expected profile CLAUDE.md to win, got %q", string(data))
	}
}

func TestGenerateMetaHash(t *testing.T) {
	cfg := setupGenerateTest(t)

	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")
	metaPath := filepath.Join(genDir, ".apm-meta.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("reading .apm-meta.json: %v", err)
	}

	var meta map[string]string
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parsing .apm-meta.json: %v", err)
	}

	hash, ok := meta["settings_hash"]
	if !ok {
		t.Fatal("expected settings_hash key in .apm-meta.json")
	}
	if len(hash) != 64 {
		t.Errorf("expected SHA-256 hash (64 hex chars), got length %d", len(hash))
	}
}

func TestGeneratePreservesRuntimeState(t *testing.T) {
	cfg := setupGenerateTest(t)

	// First generate
	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")

	// Simulate runtime state created by Claude CLI
	runtimeFiles := map[string]string{
		"history.jsonl":         `{"event":"test"}`,
		"settings.local.json":  `{"local":true}`,
		"stats-cache.json":     `{"stats":1}`,
		"statusline.sh":        `#!/bin/sh`,
	}
	for name, content := range runtimeFiles {
		if err := os.WriteFile(filepath.Join(genDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("creating runtime file %s: %v", name, err)
		}
	}

	// Create runtime directories
	runtimeDirs := []string{"sessions", "cache", "projects", "file-history"}
	for _, dir := range runtimeDirs {
		dirPath := filepath.Join(genDir, dir)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			t.Fatalf("creating runtime dir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dirPath, "data.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("writing data in %s: %v", dir, err)
		}
	}

	// Regenerate — should preserve all runtime state
	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatalf("second generate: %v", err)
	}

	// Verify runtime files are preserved
	for name, expectedContent := range runtimeFiles {
		data, err := os.ReadFile(filepath.Join(genDir, name))
		if err != nil {
			t.Errorf("runtime file %s was lost after regeneration: %v", name, err)
			continue
		}
		if string(data) != expectedContent {
			t.Errorf("runtime file %s content changed: got %q, want %q", name, string(data), expectedContent)
		}
	}

	// Verify runtime dirs are preserved
	for _, dir := range runtimeDirs {
		dataPath := filepath.Join(genDir, dir, "data.json")
		if _, err := os.Stat(dataPath); err != nil {
			t.Errorf("runtime dir %s was lost after regeneration: %v", dir, err)
		}
	}

	// Verify managed items were still regenerated properly
	if _, err := os.Stat(filepath.Join(genDir, "settings.json")); err != nil {
		t.Error("settings.json missing after regeneration")
	}
	if _, err := os.Stat(filepath.Join(genDir, ".apm-meta.json")); err != nil {
		t.Error(".apm-meta.json missing after regeneration")
	}
}

func TestCleanManagedItems(t *testing.T) {
	cfg := setupGenerateTest(t)

	// Generate first
	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")

	// Add a runtime file
	if err := os.WriteFile(filepath.Join(genDir, "history.jsonl"), []byte("runtime"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Add a runtime directory
	if err := os.MkdirAll(filepath.Join(genDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Run cleanManagedItems
	if err := cleanManagedItems(genDir); err != nil {
		t.Fatal(err)
	}

	// Managed items should be removed
	for name := range managedItemSet {
		if _, err := os.Lstat(filepath.Join(genDir, name)); err == nil {
			t.Errorf("managed item %s should have been removed", name)
		}
	}

	// Runtime items should be preserved
	if _, err := os.Stat(filepath.Join(genDir, "history.jsonl")); err != nil {
		t.Error("runtime file history.jsonl should be preserved")
	}
	if _, err := os.Stat(filepath.Join(genDir, "sessions")); err != nil {
		t.Error("runtime dir sessions should be preserved")
	}
}

func TestCleanManagedItemsPreservesRuntimeNameCollision(t *testing.T) {
	cfg := setupGenerateTest(t)

	// Add an extra to common (will become a symlink in genDir)
	if err := os.WriteFile(filepath.Join(cfg.CommonDir, "CLAUDE.md"), []byte("common"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Generate
	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("work")

	// Verify CLAUDE.md is a symlink
	fi, err := os.Lstat(filepath.Join(genDir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !isSymlink(fi) {
		t.Fatal("expected CLAUDE.md to be a symlink")
	}

	// Now replace the symlink with a regular file (simulating Claude CLI creating it)
	os.Remove(filepath.Join(genDir, "CLAUDE.md"))
	if err := os.WriteFile(filepath.Join(genDir, "CLAUDE.md"), []byte("runtime version"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Regenerate — the runtime regular file should be preserved
	// (cleanManagedItems only removes symlinks for extras, and
	// linkExtrasFrom skips when dst already exists)
	if err := generateProfile(cfg, "work"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(genDir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "runtime version" {
		t.Errorf("expected runtime version of CLAUDE.md to be preserved, got %q", string(data))
	}
}
