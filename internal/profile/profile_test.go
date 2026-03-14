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
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// Create mock ~/.claude/
	if err := os.MkdirAll(cfg.ClaudeDir, 0o755); err != nil {
		t.Fatalf("creating mock ClaudeDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "settings.json"),
		[]byte(`{"effortLevel":"high"}`), 0o644); err != nil {
		t.Fatalf("writing mock settings.json: %v", err)
	}
	for _, dir := range []string{"skills", "commands", "agents"} {
		if err := os.MkdirAll(filepath.Join(cfg.ClaudeDir, dir), 0o755); err != nil {
			t.Fatalf("creating mock %s dir: %v", dir, err)
		}
	}
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
	info, err := Get(cfg, "personal")
	if err != nil {
		t.Fatalf("Get: %v", err)
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

func TestCreateFromProfile(t *testing.T) {
	cfg := testConfig(t)
	// Create source profile first
	if err := Create(cfg, "source", "", "source profile"); err != nil {
		t.Fatalf("creating source: %v", err)
	}
	// Override settings in source
	if err := os.WriteFile(filepath.Join(cfg.ProfileDir("source"), "settings.json"),
		[]byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatalf("writing source settings: %v", err)
	}

	// Create profile from source
	if err := Create(cfg, "derived", "source", "derived profile"); err != nil {
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
	info, err := Get(cfg, "derived")
	if err != nil {
		t.Fatalf("Get derived: %v", err)
	}
	if info.Meta.Source != "source" {
		t.Errorf("source: got %s, want 'source'", info.Meta.Source)
	}
}

func TestCreateFromNonexistentProfile(t *testing.T) {
	cfg := testConfig(t)
	if err := Create(cfg, "bad", "nonexistent", ""); err == nil {
		t.Error("expected error for nonexistent source profile")
	}
	if Exists(cfg, "bad") {
		t.Error("profile directory should have been cleaned up")
	}
}

func TestCreateDuplicate(t *testing.T) {
	cfg := testConfig(t)
	if err := Create(cfg, "work", "", ""); err != nil {
		t.Fatalf("first create: %v", err)
	}
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
	if err := Create(cfg, "", "", ""); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestDelete(t *testing.T) {
	cfg := testConfig(t)
	if err := Create(cfg, "temp", "", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := Delete(cfg, "temp", false); err != nil {
		t.Fatal(err)
	}
	if Exists(cfg, "temp") {
		t.Error("profile should be deleted")
	}
}

func TestDeleteNonexistent(t *testing.T) {
	cfg := testConfig(t)
	if err := Delete(cfg, "nope", false); err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestDeleteActiveRefused(t *testing.T) {
	cfg := testConfig(t)
	if err := Create(cfg, "active", "", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := cfg.SetDefaultProfile("active"); err != nil {
		t.Fatalf("SetDefaultProfile: %v", err)
	}
	if err := Delete(cfg, "active", false); err == nil {
		t.Error("expected error deleting active profile without --force")
	}
}

func TestDeleteActiveForced(t *testing.T) {
	cfg := testConfig(t)
	if err := Create(cfg, "active", "", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := cfg.SetDefaultProfile("active"); err != nil {
		t.Fatalf("SetDefaultProfile: %v", err)
	}
	if err := Delete(cfg, "active", true); err != nil {
		t.Fatal(err)
	}
	if Exists(cfg, "active") {
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
	cfg := testConfig(t)
	if err := Create(cfg, "alpha", "", "first"); err != nil {
		t.Fatalf("Create alpha: %v", err)
	}
	if err := Create(cfg, "beta", "", "second"); err != nil {
		t.Fatalf("Create beta: %v", err)
	}
	profiles, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(profiles))
	}
}

func TestListEmpty(t *testing.T) {
	cfg := testConfig(t)
	profiles, err := List(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 profiles, got %d", len(profiles))
	}
}

func TestGetNonexistent(t *testing.T) {
	cfg := testConfig(t)
	if _, err := Get(cfg, "nope"); err == nil {
		t.Error("expected error for nonexistent profile")
	}
}

func TestExistsNonexistent(t *testing.T) {
	cfg := testConfig(t)
	if Exists(cfg, "nope") {
		t.Error("expected false for nonexistent profile")
	}
}

func TestImportWithSymlinks(t *testing.T) {
	cfg := testConfig(t)

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
	if err := Create(cfg, "symtest", "current", ""); err != nil {
		t.Fatalf("Create: %v", err)
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
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected symlink to be preserved")
	}
}

func TestCreateWithSubdirectories(t *testing.T) {
	cfg := testConfig(t)

	// Create nested structure in source
	nestedDir := filepath.Join(cfg.ClaudeDir, "commands", "sub")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("creating nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "cmd.md"), []byte("# Command"), 0o644); err != nil {
		t.Fatalf("writing nested command: %v", err)
	}

	if err := Create(cfg, "nested", "current", ""); err != nil {
		t.Fatalf("Create: %v", err)
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
