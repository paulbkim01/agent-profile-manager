package config

import (
	"os"
	"path/filepath"
	"testing"
)

func testConfig(t *testing.T) *Config {
	t.Helper()
	dir := t.TempDir()
	return &Config{
		APMDir:       dir,
		ClaudeDir:    filepath.Join(dir, ".claude"),
		CommonDir:    filepath.Join(dir, "common"),
		ProfilesDir:  filepath.Join(dir, "profiles"),
		GeneratedDir: filepath.Join(dir, "generated"),
		ConfigPath:   filepath.Join(dir, "config.yaml"),
	}
}

func TestLoadWithOverride(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.APMDir != dir {
		t.Errorf("expected APMDir=%s, got %s", dir, cfg.APMDir)
	}
}

func TestDefaultProfileEmpty(t *testing.T) {
	cfg := testConfig(t)

	name, err := cfg.DefaultProfile()
	if err != nil {
		t.Fatalf("DefaultProfile failed: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty default profile, got %q", name)
	}
}

func TestSetAndGetDefaultProfile(t *testing.T) {
	cfg := testConfig(t)

	if err := cfg.SetDefaultProfile("work"); err != nil {
		t.Fatalf("SetDefaultProfile failed: %v", err)
	}

	name, err := cfg.DefaultProfile()
	if err != nil {
		t.Fatalf("DefaultProfile failed: %v", err)
	}
	if name != "work" {
		t.Errorf("expected 'work', got %q", name)
	}
}

func TestClearDefaultProfile(t *testing.T) {
	cfg := testConfig(t)

	if err := cfg.SetDefaultProfile("work"); err != nil {
		t.Fatalf("SetDefaultProfile failed: %v", err)
	}
	if err := cfg.ClearDefaultProfile(); err != nil {
		t.Fatalf("ClearDefaultProfile failed: %v", err)
	}

	name, err := cfg.DefaultProfile()
	if err != nil {
		t.Fatalf("DefaultProfile failed: %v", err)
	}
	if name != "" {
		t.Errorf("expected empty after clear, got %q", name)
	}
}

func TestEnsureDirs(t *testing.T) {
	cfg := testConfig(t)

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}

	for _, d := range []string{cfg.CommonDir, cfg.ProfilesDir, cfg.GeneratedDir} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("expected dir %s to exist: %v", d, err)
		}
	}

	// common/settings.json should exist
	settingsPath := filepath.Join(cfg.CommonDir, "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("expected common/settings.json: %v", err)
	}
}

func TestEnsureDirsIdempotent(t *testing.T) {
	cfg := testConfig(t)

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("first EnsureDirs failed: %v", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("second EnsureDirs failed: %v", err)
	}
}

func TestProfileDir(t *testing.T) {
	cfg := testConfig(t)
	got := cfg.ProfileDir("work")
	want := filepath.Join(cfg.ProfilesDir, "work")
	if got != want {
		t.Errorf("ProfileDir = %q, want %q", got, want)
	}
}

func TestGeneratedProfileDir(t *testing.T) {
	cfg := testConfig(t)
	got := cfg.GeneratedProfileDir("work")
	want := filepath.Join(cfg.GeneratedDir, "work")
	if got != want {
		t.Errorf("GeneratedProfileDir = %q, want %q", got, want)
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("{{invalid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoadWithClaudeDirOverride(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("claude_dir: /tmp/custom-claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.ClaudeDir != "/tmp/custom-claude" {
		t.Errorf("expected ClaudeDir=/tmp/custom-claude, got %s", cfg.ClaudeDir)
	}
}
