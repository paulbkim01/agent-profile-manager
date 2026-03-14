package main

import (
	"path/filepath"
	"testing"
)

// newTestConfig creates a Config backed by a temp directory with all
// standard dirs created via EnsureDirs(). It sandboxes HOME so that
// defaultClaudeDir() and defaultAPMDir() never touch real ~/.claude.
// Use this as the base for all test setup helpers.
func newTestConfig(t *testing.T) *Config {
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
		t.Fatalf("EnsureDirs: %v", err)
	}
	return cfg
}
