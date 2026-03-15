package main

import (
	"path/filepath"
	"testing"
)

// newTestConfig creates a Config backed by a temp directory with all
// standard dirs created via EnsureDirs(). Use this as the base for
// all test setup helpers.
func newTestConfig(t *testing.T) *Config {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{
		APMDir:       dir,
		ClaudeDir:    filepath.Join(dir, ".claude"),
		CommonDir:    filepath.Join(dir, "common"),
		ProfilesDir:  filepath.Join(dir, "profiles"),
		GeneratedDir: filepath.Join(dir, "generated"),
		ConfigPath:   filepath.Join(dir, "config.yaml"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	return cfg
}
