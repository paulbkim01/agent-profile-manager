package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// setupActivateTest creates a fake HOME with ~/.claude containing some content,
// an APM config dir with one profile, and returns the Config and fake home path.
func setupActivateTest(t *testing.T) (*Config, string) {
	t.Helper()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	apmDir := filepath.Join(fakeHome, ".config", "apm")
	cfg := &Config{
		APMDir:       apmDir,
		ClaudeDir:    filepath.Join(fakeHome, ".claude"),
		CommonDir:    filepath.Join(apmDir, "common"),
		ProfilesDir:  filepath.Join(apmDir, "profiles"),
		GeneratedDir: filepath.Join(apmDir, "generated"),
		ConfigPath:   filepath.Join(apmDir, "config.yaml"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Create real ~/.claude with some content
	if err := os.MkdirAll(cfg.ClaudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "some-file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a test profile
	if err := createProfile(cfg, "test-profile", "", "test"); err != nil {
		t.Fatal(err)
	}

	return cfg, fakeHome
}

func TestActivateFirstTime(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)
	claudePath := filepath.Join(fakeHome, ".claude")

	if err := activateProfile(cfg, "test-profile", false); err != nil {
		t.Fatalf("activateProfile failed: %v", err)
	}

	// Should be a symlink
	fi, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf("lstat %s: %v", claudePath, err)
	}
	if !isSymlink(fi) {
		t.Errorf("%s should be a symlink", claudePath)
	}

	// Should point to generated dir
	target, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != cfg.GeneratedProfileDir("test-profile") {
		t.Errorf("symlink target = %s, want %s", target, cfg.GeneratedProfileDir("test-profile"))
	}

	// Backup should exist with original content
	backupPath := filepath.Join(cfg.APMDir, claudeHomeDirName)
	if _, err := os.Stat(filepath.Join(backupPath, "some-file.txt")); err != nil {
		t.Errorf("backup should contain original files: %v", err)
	}

	// Active profile should be set in config
	active, err := cfg.ActiveProfile()
	if err != nil {
		t.Fatalf("ActiveProfile: %v", err)
	}
	if active != "test-profile" {
		t.Errorf("active profile = %q, want %q", active, "test-profile")
	}

	// ClaudeDir should now point to backup
	if cfg.ClaudeDir != backupPath {
		t.Errorf("cfg.ClaudeDir = %s, want %s", cfg.ClaudeDir, backupPath)
	}
}

func TestActivateSkipSymlink(t *testing.T) {
	cfg, _ := setupActivateTest(t)

	err := activateProfile(cfg, "test-profile", true)
	if !errors.Is(err, errSkipSymlink) {
		t.Errorf("expected errSkipSymlink, got: %v", err)
	}
}

func TestActivateSwitchProfile(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)
	claudePath := filepath.Join(fakeHome, ".claude")

	// Create second profile
	if err := createProfile(cfg, "other-profile", "", "other"); err != nil {
		t.Fatal(err)
	}

	// Activate first profile
	if err := activateProfile(cfg, "test-profile", false); err != nil {
		t.Fatalf("activate first: %v", err)
	}

	// Switch to second profile
	if err := activateProfile(cfg, "other-profile", false); err != nil {
		t.Fatalf("activate second: %v", err)
	}

	// Should be a symlink to the second profile
	target, err := os.Readlink(claudePath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != cfg.GeneratedProfileDir("other-profile") {
		t.Errorf("symlink target = %s, want %s", target, cfg.GeneratedProfileDir("other-profile"))
	}

	// Active profile should be updated
	active, _ := cfg.ActiveProfile()
	if active != "other-profile" {
		t.Errorf("active profile = %q, want %q", active, "other-profile")
	}
}

func TestDeactivateRestores(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)
	claudePath := filepath.Join(fakeHome, ".claude")

	// Activate
	if err := activateProfile(cfg, "test-profile", false); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Deactivate
	if err := deactivateProfile(cfg); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Should be a real directory again
	fi, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf("lstat %s: %v", claudePath, err)
	}
	if isSymlink(fi) {
		t.Errorf("%s should not be a symlink after deactivation", claudePath)
	}
	if !fi.IsDir() {
		t.Errorf("%s should be a directory after deactivation", claudePath)
	}

	// Should contain original content
	if _, err := os.Stat(filepath.Join(claudePath, "some-file.txt")); err != nil {
		t.Errorf("original files should be restored: %v", err)
	}

	// Active profile should be cleared
	active, err := cfg.ActiveProfile()
	if err != nil {
		t.Fatalf("ActiveProfile: %v", err)
	}
	if active != "" {
		t.Errorf("active profile should be empty, got %q", active)
	}

	// Claude home should be cleared
	home, err := cfg.ClaudeHome()
	if err != nil {
		t.Fatalf("ClaudeHome: %v", err)
	}
	if home != "" {
		t.Errorf("claude_home should be empty, got %q", home)
	}
}

func TestActivateNoClaudeDir(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	apmDir := filepath.Join(fakeHome, ".config", "apm")
	cfg := &Config{
		APMDir:       apmDir,
		ClaudeDir:    filepath.Join(fakeHome, ".claude"),
		CommonDir:    filepath.Join(apmDir, "common"),
		ProfilesDir:  filepath.Join(apmDir, "profiles"),
		GeneratedDir: filepath.Join(apmDir, "generated"),
		ConfigPath:   filepath.Join(apmDir, "config.yaml"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if err := createProfile(cfg, "test-profile", "", "test"); err != nil {
		t.Fatal(err)
	}

	// Don't create ~/.claude — it doesn't exist

	if err := activateProfile(cfg, "test-profile", false); err != nil {
		t.Fatalf("activateProfile failed: %v", err)
	}

	claudePath := filepath.Join(fakeHome, ".claude")

	// Should be a symlink
	fi, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if !isSymlink(fi) {
		t.Errorf("%s should be a symlink", claudePath)
	}

	// Empty backup should exist
	backupPath := filepath.Join(apmDir, claudeHomeDirName)
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("backup dir should exist: %v", err)
	}
}

func TestDeactivateNoState(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	apmDir := filepath.Join(fakeHome, ".config", "apm")
	cfg := &Config{
		APMDir:       apmDir,
		ClaudeDir:    filepath.Join(fakeHome, ".claude"),
		CommonDir:    filepath.Join(apmDir, "common"),
		ProfilesDir:  filepath.Join(apmDir, "profiles"),
		GeneratedDir: filepath.Join(apmDir, "generated"),
		ConfigPath:   filepath.Join(apmDir, "config.yaml"),
	}
	if err := os.MkdirAll(apmDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Deactivate without prior activation — should be a no-op
	if err := deactivateProfile(cfg); err != nil {
		t.Fatalf("deactivateProfile should be a no-op, got: %v", err)
	}
}

func TestCaptureRestoreExternalState(t *testing.T) {
	dir := t.TempDir()
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create a fake ~/.claude.json
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	content := `{"oauthTokens":{"prod":"token123"}}`
	if err := os.WriteFile(claudeJSON, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Capture
	targetDir := filepath.Join(dir, "external")
	if err := captureExternalState(targetDir); err != nil {
		t.Fatalf("captureExternalState: %v", err)
	}

	// Verify captured file
	captured, err := os.ReadFile(filepath.Join(targetDir, "claude.json"))
	if err != nil {
		t.Fatalf("reading captured file: %v", err)
	}
	if string(captured) != content {
		t.Errorf("captured content = %q, want %q", string(captured), content)
	}

	// Modify ~/.claude.json to verify restore overwrites
	if err := os.WriteFile(claudeJSON, []byte(`{"different":"content"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restore
	if err := restoreExternalState(targetDir); err != nil {
		t.Fatalf("restoreExternalState: %v", err)
	}

	// Verify restored
	restored, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != content {
		t.Errorf("restored content = %q, want %q", string(restored), content)
	}
}

func TestRestoreNoSavedState(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create ~/.claude.json with original content
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	original := `{"keep":"me"}`
	if err := os.WriteFile(claudeJSON, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// Restore from non-existent dir — should remove ~/.claude.json for fresh setup
	if err := restoreExternalState(filepath.Join(fakeHome, "nonexistent")); err != nil {
		t.Fatalf("restoreExternalState: %v", err)
	}

	// ~/.claude.json should be removed so Claude prompts for first-time setup
	if _, err := os.Stat(claudeJSON); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("~/.claude.json should be removed for fresh setup, but still exists")
	}
}

func TestRestoreNoSavedStateNoExistingFile(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// No ~/.claude.json exists — should not error
	if err := restoreExternalState(filepath.Join(fakeHome, "nonexistent")); err != nil {
		t.Fatalf("restoreExternalState: %v", err)
	}
}

func TestCaptureExternalStateNoClaudeJSON(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	// No ~/.claude.json exists

	targetDir := filepath.Join(fakeHome, "target")
	if err := captureExternalState(targetDir); err != nil {
		t.Fatalf("should succeed gracefully when no ~/.claude.json exists: %v", err)
	}

	// Target dir should not have been created (nothing to capture)
	if _, err := os.Stat(filepath.Join(targetDir, "claude.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("should not create claude.json when source doesn't exist")
	}
}

func TestSwitchPreservesOldProfileState(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)

	// Create second profile
	if err := createProfile(cfg, "other-profile", "", "other"); err != nil {
		t.Fatal(err)
	}

	// Create ~/.claude.json
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"profile":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Activate first profile
	if err := activateProfile(cfg, "test-profile", false); err != nil {
		t.Fatalf("activate first: %v", err)
	}

	genDir := cfg.GeneratedProfileDir("test-profile")

	// Simulate runtime state in profile A's generated dir
	if err := os.WriteFile(filepath.Join(genDir, "history.jsonl"), []byte("profile-a-history"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Update ~/.claude.json while on profile A
	if err := os.WriteFile(claudeJSON, []byte(`{"profile":"test-updated"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Switch to profile B
	if err := activateProfile(cfg, "other-profile", false); err != nil {
		t.Fatalf("switch to other: %v", err)
	}

	// Profile A's runtime state should still be in its generated dir
	data, err := os.ReadFile(filepath.Join(genDir, "history.jsonl"))
	if err != nil {
		t.Fatalf("profile A runtime state lost: %v", err)
	}
	if string(data) != "profile-a-history" {
		t.Errorf("profile A runtime state changed: %q", string(data))
	}

	// Profile A's external state should have been captured
	extState := filepath.Join(cfg.ExternalStateDir("test-profile"), "claude.json")
	data, err = os.ReadFile(extState)
	if err != nil {
		t.Fatalf("profile A external state not captured: %v", err)
	}
	if string(data) != `{"profile":"test-updated"}` {
		t.Errorf("profile A external state = %q, want updated content", string(data))
	}

	// Profile B has no saved external state — ~/.claude.json should be removed
	claudeJSON = filepath.Join(fakeHome, ".claude.json")
	if _, err := os.Stat(claudeJSON); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("~/.claude.json should be removed when switching to new profile, but still exists")
	}
}

func TestDeactivatePreservesGeneratedDirs(t *testing.T) {
	cfg, _ := setupActivateTest(t)

	// Activate
	if err := activateProfile(cfg, "test-profile", false); err != nil {
		t.Fatalf("activate: %v", err)
	}

	genDir := cfg.GeneratedProfileDir("test-profile")

	// Add runtime state
	if err := os.WriteFile(filepath.Join(genDir, "history.jsonl"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Deactivate
	if err := deactivateProfile(cfg); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Generated dir should still exist with runtime state preserved
	data, err := os.ReadFile(filepath.Join(genDir, "history.jsonl"))
	if err != nil {
		t.Fatalf("generated dir or runtime state was removed: %v", err)
	}
	if string(data) != "keep me" {
		t.Errorf("runtime state changed: %q", string(data))
	}
}

func TestBackupIncludesExternalState(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	apmDir := filepath.Join(fakeHome, ".config", "apm")
	cfg := &Config{
		APMDir:       apmDir,
		ClaudeDir:    filepath.Join(fakeHome, ".claude"),
		CommonDir:    filepath.Join(apmDir, "common"),
		ProfilesDir:  filepath.Join(apmDir, "profiles"),
		GeneratedDir: filepath.Join(apmDir, "generated"),
		ConfigPath:   filepath.Join(apmDir, "config.yaml"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Create ~/.claude as a real directory
	if err := os.MkdirAll(cfg.ClaudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create ~/.claude.json
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"auth":"token"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run backup
	if err := backupClaude(cfg); err != nil {
		t.Fatalf("backupClaude: %v", err)
	}

	// Verify external state was captured
	extBackup := filepath.Join(cfg.BackupExternalDir(), "claude.json")
	data, err := os.ReadFile(extBackup)
	if err != nil {
		t.Fatalf("external state not captured in backup: %v", err)
	}
	if string(data) != `{"auth":"token"}` {
		t.Errorf("backup external state = %q, want original", string(data))
	}
}

func TestActivateDeactivateRoundtrip(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)
	claudePath := filepath.Join(fakeHome, ".claude")

	// Activate
	if err := activateProfile(cfg, "test-profile", false); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Verify symlink works (can read generated content through it)
	entries, err := os.ReadDir(claudePath)
	if err != nil {
		t.Fatalf("readdir through symlink: %v", err)
	}
	if len(entries) == 0 {
		t.Error("generated dir should have content")
	}

	// Deactivate
	if err := deactivateProfile(cfg); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Re-activate (should work again)
	if err := activateProfile(cfg, "test-profile", false); err != nil {
		t.Fatalf("re-activate: %v", err)
	}

	fi, _ := os.Lstat(claudePath)
	if !isSymlink(fi) {
		t.Errorf("should be a symlink after re-activation")
	}
}
