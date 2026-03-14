package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureDirsCreatesStatusLine(t *testing.T) {
	cfg := newTestConfig(t)

	// statusline.sh should exist and be executable
	slPath := filepath.Join(cfg.CommonDir, "statusline.sh")
	fi, err := os.Stat(slPath)
	if err != nil {
		t.Fatalf("statusline.sh not created: %v", err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Error("statusline.sh should be executable")
	}

	data, err := os.ReadFile(slPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "#!/") {
		t.Error("statusline.sh should start with a shebang")
	}
}

func TestEnsureDirsDefaultSettingsHasStatusLine(t *testing.T) {
	cfg := newTestConfig(t)

	data, err := os.ReadFile(filepath.Join(cfg.CommonDir, "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings.json invalid JSON: %v", err)
	}

	sl, ok := settings["statusLine"]
	if !ok {
		t.Fatal("settings.json missing statusLine key")
	}
	slMap, ok := sl.(map[string]any)
	if !ok {
		t.Fatal("statusLine should be an object")
	}
	if slMap["type"] != "command" {
		t.Errorf("statusLine.type = %v, want 'command'", slMap["type"])
	}
	cmd, _ := slMap["command"].(string)
	if !strings.Contains(cmd, "statusline.sh") {
		t.Errorf("statusLine.command should reference statusline.sh, got: %s", cmd)
	}
}

func TestStatusLineNotOverwritten(t *testing.T) {
	cfg := newTestConfig(t)

	// Write custom content
	slPath := filepath.Join(cfg.CommonDir, "statusline.sh")
	custom := []byte("#!/bin/bash\necho custom")
	if err := os.WriteFile(slPath, custom, 0o755); err != nil {
		t.Fatal(err)
	}

	// Run EnsureDirs again
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	// Should still be custom
	data, err := os.ReadFile(slPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(custom) {
		t.Error("statusline.sh was overwritten; should preserve user customizations")
	}
}

func TestDefaultStatusLineSettings(t *testing.T) {
	data := defaultStatusLineSettings("/fake/common")

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("defaultStatusLineSettings produced invalid JSON: %v", err)
	}

	sl := settings["statusLine"].(map[string]any)
	if sl["type"] != "command" {
		t.Errorf("type = %v, want 'command'", sl["type"])
	}
	cmd := sl["command"].(string)
	if !strings.Contains(cmd, "statusline.sh") {
		t.Errorf("command should reference statusline.sh, got: %s", cmd)
	}
	if !strings.Contains(cmd, "/fake/common") {
		t.Errorf("command should contain the common dir path, got: %s", cmd)
	}
}

func TestStatusLinePatchesExistingSettings(t *testing.T) {
	cfg := newTestConfig(t)

	// Overwrite settings.json with existing user config (no statusLine)
	settingsPath := filepath.Join(cfg.CommonDir, "settings.json")
	os.WriteFile(settingsPath, []byte(`{"fastMode": true}`+"\n"), 0o644)

	// Remove statusline.sh so writeDefaultStatusLine triggers a fresh write
	os.Remove(filepath.Join(cfg.CommonDir, "statusline.sh"))

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// statusLine should be injected
	if _, ok := settings["statusLine"]; !ok {
		t.Fatal("statusLine not injected into existing settings.json")
	}
	// existing keys should be preserved
	if settings["fastMode"] != true {
		t.Error("existing fastMode key was lost")
	}
}

func TestStatusLinePreservesCustomConfig(t *testing.T) {
	cfg := newTestConfig(t)

	// Write settings with a custom statusLine already present
	settingsPath := filepath.Join(cfg.CommonDir, "settings.json")
	custom := `{"statusLine": {"type": "command", "command": "my-script"}}` + "\n"
	os.WriteFile(settingsPath, []byte(custom), 0o644)

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	// Should not have been modified
	if string(data) != custom {
		t.Errorf("custom statusLine was overwritten:\n  got:  %s\n  want: %s", string(data), custom)
	}
}
