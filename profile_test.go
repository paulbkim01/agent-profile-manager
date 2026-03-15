package main

import (
	"os"
	"path/filepath"
	"testing"
)

func testProfileConfig(t *testing.T) *Config {
	t.Helper()
	cfg := newTestConfig(t)

	// Create mock ~/.claude/ with settings and managed dirs
	if err := os.MkdirAll(cfg.ClaudeDir, 0o755); err != nil {
		t.Fatalf("creating mock ClaudeDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "settings.json"),
		[]byte(`{"effortLevel":"high"}`), 0o644); err != nil {
		t.Fatalf("writing mock settings.json: %v", err)
	}
	for _, dir := range managedDirs {
		if err := os.MkdirAll(filepath.Join(cfg.ClaudeDir, dir), 0o755); err != nil {
			t.Fatalf("creating mock %s dir: %v", dir, err)
		}
	}
	return cfg
}

func TestCreateEmpty(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "personal", "", "my personal profile"); err != nil {
		t.Fatal(err)
	}
	if !profileExists(cfg, "personal") {
		t.Error("profile should exist")
	}
	info, err := getProfile(cfg, "personal")
	if err != nil {
		t.Fatalf("getProfile: %v", err)
	}
	if info.Meta.Name != "personal" {
		t.Errorf("name: got %s, want personal", info.Meta.Name)
	}
	if info.Meta.Description != "my personal profile" {
		t.Errorf("description: got %s, want 'my personal profile'", info.Meta.Description)
	}

	// Verify settings.json is valid empty JSON
	data, err := os.ReadFile(filepath.Join(cfg.ProfileDir("personal"), "settings.json"))
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	if string(data) != "{}\n" {
		t.Errorf("settings.json: got %q, want %q", string(data), "{}\n")
	}
}

func TestCreateFromCurrent(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "work", "current", ""); err != nil {
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

func TestCreateFromProfile(t *testing.T) {
	cfg := testProfileConfig(t)
	// Create source profile first
	if err := createProfile(cfg, "source", "", "source profile"); err != nil {
		t.Fatalf("creating source: %v", err)
	}
	// Override settings in source
	if err := os.WriteFile(filepath.Join(cfg.ProfileDir("source"), "settings.json"),
		[]byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatalf("writing source settings: %v", err)
	}

	// Create profile from source
	if err := createProfile(cfg, "derived", "source", "derived profile"); err != nil {
		t.Fatalf("creating derived: %v", err)
	}

	// Verify settings were copied
	data, err := os.ReadFile(filepath.Join(cfg.ProfileDir("derived"), "settings.json"))
	if err != nil {
		t.Fatalf("reading derived settings: %v", err)
	}
	if string(data) != `{"model":"opus"}` {
		t.Errorf("derived settings: got %s", string(data))
	}

	// Verify source is recorded in metadata
	info, err := getProfile(cfg, "derived")
	if err != nil {
		t.Fatalf("getProfile derived: %v", err)
	}
	if info.Meta.Source != "source" {
		t.Errorf("source: got %s, want 'source'", info.Meta.Source)
	}
}

func TestCreateFromNonexistentProfile(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "bad", "nonexistent", ""); err == nil {
		t.Error("expected error for nonexistent source profile")
	}
	if profileExists(cfg, "bad") {
		t.Error("profile directory should have been cleaned up")
	}
}

func TestCreateDuplicate(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "work", "", ""); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := createProfile(cfg, "work", "", ""); err == nil {
		t.Error("expected error for duplicate")
	}
}

func TestCreateInvalidName(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "common", "", ""); err == nil {
		t.Error("expected error for reserved name")
	}
	if err := createProfile(cfg, "My Profile", "", ""); err == nil {
		t.Error("expected error for invalid name")
	}
	if err := createProfile(cfg, "", "", ""); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestDelete(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "temp", "", ""); err != nil {
		t.Fatalf("createProfile: %v", err)
	}
	if err := deleteProfile(cfg, "temp", false); err != nil {
		t.Fatal(err)
	}
	if profileExists(cfg, "temp") {
		t.Error("profile should be deleted")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := deleteProfile(cfg, "nope", false); err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestDeleteActiveRefused(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "active", "", ""); err != nil {
		t.Fatalf("createProfile: %v", err)
	}
	if err := cfg.SetDefaultProfile("active"); err != nil {
		t.Fatalf("SetDefaultProfile: %v", err)
	}
	if err := deleteProfile(cfg, "active", false); err == nil {
		t.Error("expected error deleting active profile without --force")
	}
}

func TestDeleteActiveForced(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "active", "", ""); err != nil {
		t.Fatalf("createProfile: %v", err)
	}
	if err := cfg.SetDefaultProfile("active"); err != nil {
		t.Fatalf("SetDefaultProfile: %v", err)
	}
	if err := deleteProfile(cfg, "active", true); err != nil {
		t.Fatal(err)
	}
	if profileExists(cfg, "active") {
		t.Error("profile should be deleted")
	}
	defaultProfile, err := cfg.DefaultProfile()
	if err != nil {
		t.Fatalf("DefaultProfile: %v", err)
	}
	if defaultProfile != "" {
		t.Errorf("default should be cleared, got %s", defaultProfile)
	}
}

func TestList(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "alpha", "", "first"); err != nil {
		t.Fatalf("createProfile alpha: %v", err)
	}
	if err := createProfile(cfg, "beta", "", "second"); err != nil {
		t.Fatalf("createProfile beta: %v", err)
	}
	profiles, err := listProfiles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(profiles))
	}
}

func TestListEmpty(t *testing.T) {
	cfg := testProfileConfig(t)
	profiles, err := listProfiles(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestGetNonexistent(t *testing.T) {
	cfg := testProfileConfig(t)
	if _, err := getProfile(cfg, "nope"); err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestExistsNonexistent(t *testing.T) {
	cfg := testProfileConfig(t)
	if profileExists(cfg, "nope") {
		t.Error("expected false for nonexistent profile")
	}
}

func TestImportWithSymlinks(t *testing.T) {
	cfg := testProfileConfig(t)

	// Create a symlink in the source skills dir
	skillsDir := filepath.Join(cfg.ClaudeDir, "skills")
	targetFile := filepath.Join(cfg.ClaudeDir, "skills", "real-skill.md")
	if err := os.WriteFile(targetFile, []byte("# Skill"), 0o644); err != nil {
		t.Fatalf("writing skill file: %v", err)
	}
	linkPath := filepath.Join(skillsDir, "linked-skill.md")
	if err := os.Symlink(targetFile, linkPath); err != nil {
		t.Fatalf("creating symlink: %v", err)
	}

	// Create profile from current
	if err := createProfile(cfg, "symtest", "current", ""); err != nil {
		t.Fatalf("createProfile: %v", err)
	}

	// Verify the real file was copied
	copiedReal := filepath.Join(cfg.ProfileDir("symtest"), "skills", "real-skill.md")
	if _, err := os.Stat(copiedReal); err != nil {
		t.Errorf("real skill file not copied: %v", err)
	}

	// Verify the symlink was preserved as a symlink
	copiedLink := filepath.Join(cfg.ProfileDir("symtest"), "skills", "linked-skill.md")
	info, err := os.Lstat(copiedLink)
	if err != nil {
		t.Fatalf("symlink not copied: %v", err)
	}
	if !isSymlink(info) {
		t.Error("expected symlink to be preserved")
	}
}

func TestCreateWithSubdirectories(t *testing.T) {
	cfg := testProfileConfig(t)

	// Create nested structure in source
	nestedDir := filepath.Join(cfg.ClaudeDir, "commands", "sub")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("creating nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "cmd.md"), []byte("# Command"), 0o644); err != nil {
		t.Fatalf("writing nested command: %v", err)
	}

	if err := createProfile(cfg, "nested", "current", ""); err != nil {
		t.Fatalf("createProfile: %v", err)
	}

	// Verify nested file was copied
	copied := filepath.Join(cfg.ProfileDir("nested"), "commands", "sub", "cmd.md")
	data, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("reading nested file: %v", err)
	}
	if string(data) != "# Command" {
		t.Errorf("nested file content: got %q", string(data))
	}
}
