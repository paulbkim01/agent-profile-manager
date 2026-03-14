package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestDeleteCleansGeneratedDir(t *testing.T) {
	cfg := testProfileConfig(t)
	if err := createProfile(cfg, "temp", "", ""); err != nil {
		t.Fatalf("createProfile: %v", err)
	}

	// Generate the profile to create a generated dir
	if err := generateProfile(cfg, "temp"); err != nil {
		t.Fatal(err)
	}
	genDir := cfg.GeneratedProfileDir("temp")

	// Add some runtime state to the generated dir
	if err := os.WriteFile(filepath.Join(genDir, "history.jsonl"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Delete the profile
	if err := deleteProfile(cfg, "temp", false); err != nil {
		t.Fatal(err)
	}

	// Generated dir should be removed
	if _, err := os.Stat(genDir); !errors.Is(err, os.ErrNotExist) {
		t.Error("generated dir should be removed after delete")
	}

	// Profile dir should be removed
	if profileExists(cfg, "temp") {
		t.Error("profile should be deleted")
	}
}

func TestSeedRuntimeState(t *testing.T) {
	cfg := testProfileConfig(t)

	// Add runtime files to claude dir
	if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "history.jsonl"), []byte("history"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.ClaudeDir, "sessions", "abc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ClaudeDir, "sessions", "abc", "data.json"), []byte("session"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create target dir with settings.json (managed item — should be skipped)
	genDir := filepath.Join(cfg.GeneratedDir, "test")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "settings.json"), []byte("generated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Seed runtime state
	if err := seedRuntimeState(cfg.ClaudeDir, genDir); err != nil {
		t.Fatal(err)
	}

	// Runtime files should be copied
	data, err := os.ReadFile(filepath.Join(genDir, "history.jsonl"))
	if err != nil {
		t.Fatalf("history.jsonl not seeded: %v", err)
	}
	if string(data) != "history" {
		t.Errorf("history.jsonl = %q, want %q", string(data), "history")
	}

	// Runtime dirs should be copied
	data, err = os.ReadFile(filepath.Join(genDir, "sessions", "abc", "data.json"))
	if err != nil {
		t.Fatalf("sessions not seeded: %v", err)
	}
	if string(data) != "session" {
		t.Errorf("session data = %q, want %q", string(data), "session")
	}

	// Managed items should NOT be overwritten
	data, err = os.ReadFile(filepath.Join(genDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "generated" {
		t.Errorf("settings.json should be preserved, got %q", string(data))
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

// Fix 3: Verify importFrom copies env file
func TestImportFromCopiesEnvFile(t *testing.T) {
	cfg := testProfileConfig(t)

	// Create source profile with an env file
	if err := createProfile(cfg, "env-source", "", ""); err != nil {
		t.Fatal(err)
	}
	envContent := "API_KEY=secret123\nREGION=us-east-1\n"
	if err := os.WriteFile(filepath.Join(cfg.ProfileDir("env-source"), envFileName), []byte(envContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create profile from the source
	if err := createProfile(cfg, "env-derived", "env-source", ""); err != nil {
		t.Fatal(err)
	}

	// Verify env file was copied
	data, err := os.ReadFile(filepath.Join(cfg.ProfileDir("env-derived"), envFileName))
	if err != nil {
		t.Fatalf("env file not copied: %v", err)
	}
	if string(data) != envContent {
		t.Errorf("env file content = %q, want %q", string(data), envContent)
	}
}

// Fix 3: Verify importFrom copies extra files
func TestImportFromCopiesExtras(t *testing.T) {
	cfg := testProfileConfig(t)

	// Create source profile with an extra file
	if err := createProfile(cfg, "extra-source", "", ""); err != nil {
		t.Fatal(err)
	}
	extraContent := "custom data"
	if err := os.WriteFile(filepath.Join(cfg.ProfileDir("extra-source"), "custom-config.txt"), []byte(extraContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create profile from the source
	if err := createProfile(cfg, "extra-derived", "extra-source", ""); err != nil {
		t.Fatal(err)
	}

	// Verify extra file was copied
	data, err := os.ReadFile(filepath.Join(cfg.ProfileDir("extra-derived"), "custom-config.txt"))
	if err != nil {
		t.Fatalf("extra file not copied: %v", err)
	}
	if string(data) != extraContent {
		t.Errorf("extra file content = %q, want %q", string(data), extraContent)
	}
}

// ---------------------------------------------------------------------------
// Validation tests
// ---------------------------------------------------------------------------

func TestProfileName(t *testing.T) {
	valid := []string{
		"work",
		"personal",
		"my-work",
		"dev-2",
		"a",
		"test-profile-123",
		"default",
	}
	for _, name := range valid {
		if err := validateProfileName(name); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}

	invalid := []struct {
		name string
		desc string
	}{
		{"", "empty"},
		{"Work", "uppercase"},
		{"my_work", "underscore"},
		{"my work", "space"},
		{"-leading", "leading hyphen"},
		{"com/mon", "slash"},
		{"a.b", "dot"},
		{"common", "reserved name"},
		{"generated", "reserved name"},
		{"config", "reserved name"},
		{"current", "reserved name"},
	}
	for _, tc := range invalid {
		if err := validateProfileName(tc.name); err == nil {
			t.Errorf("expected %q (%s) to be invalid", tc.name, tc.desc)
		}
	}

	// Length limit
	long := ""
	for i := 0; i < 51; i++ {
		long += "a"
	}
	if err := validateProfileName(long); err == nil {
		t.Errorf("expected 51-char name to be invalid")
	}
	if err := validateProfileName(long[:50]); err != nil {
		t.Errorf("expected 50-char name to be valid, got: %v", err)
	}
}

func TestSettingsJSON(t *testing.T) {
	// Valid
	valid := filepath.Join(t.TempDir(), "ok.json")
	os.WriteFile(valid, []byte(`{"model":"opus"}`), 0o644)
	if err := validateSettingsJSON(valid); err != nil {
		t.Errorf("expected valid: %v", err)
	}

	// Empty object is valid
	empty := filepath.Join(t.TempDir(), "empty.json")
	os.WriteFile(empty, []byte(`{}`), 0o644)
	if err := validateSettingsJSON(empty); err != nil {
		t.Errorf("expected valid: %v", err)
	}

	// Invalid JSON
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{broken`), 0o644)
	if err := validateSettingsJSON(bad); err == nil {
		t.Error("expected error for invalid JSON")
	}

	// Array (not an object)
	arr := filepath.Join(t.TempDir(), "arr.json")
	os.WriteFile(arr, []byte(`[1,2,3]`), 0o644)
	if err := validateSettingsJSON(arr); err == nil {
		t.Error("expected error for array")
	}

	// Null
	nullFile := filepath.Join(t.TempDir(), "null.json")
	os.WriteFile(nullFile, []byte(`null`), 0o644)
	if err := validateSettingsJSON(nullFile); err == nil {
		t.Error("expected error for null")
	}

	// Scalar string
	str := filepath.Join(t.TempDir(), "str.json")
	os.WriteFile(str, []byte(`"hello"`), 0o644)
	if err := validateSettingsJSON(str); err == nil {
		t.Error("expected error for string scalar")
	}

	// Missing file is valid (treated as empty {})
	if err := validateSettingsJSON("/nonexistent"); err != nil {
		t.Errorf("expected missing file to be valid, got: %v", err)
	}
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"work", "work", 0},
		{"work", "wrk", 1},
		{"work", "wrok", 2},
		{"work", "wurk", 1},
		{"work", "personal", 7},
		{"Work", "work", 0}, // case-insensitive
	}
	for _, tc := range tests {
		got := levenshtein(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSuggestProfile(t *testing.T) {
	cfg := newTestConfig(t)

	// Create profiles
	if err := createProfile(cfg, "work", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := createProfile(cfg, "personal", "", ""); err != nil {
		t.Fatal(err)
	}

	// Close match should suggest
	hint := suggestProfile(cfg, "wrk")
	if !strings.Contains(hint, "work") {
		t.Errorf("expected 'work' suggestion for 'wrk', got: %q", hint)
	}

	// Exact match is distance 0, should suggest
	hint = suggestProfile(cfg, "work")
	if !strings.Contains(hint, "work") {
		t.Errorf("expected 'work' suggestion for exact match, got: %q", hint)
	}

	// Distant name should not suggest
	hint = suggestProfile(cfg, "foobar")
	if hint != "" {
		t.Errorf("expected no suggestion for 'foobar', got: %q", hint)
	}
}

// ---------------------------------------------------------------------------
// Merge tests
// ---------------------------------------------------------------------------

func TestScalarOverride(t *testing.T) {
	common := map[string]any{
		"model":       "claude-sonnet-4-6",
		"effortLevel": "high",
	}
	profile := map[string]any{
		"model": "us.anthropic.claude-opus-4-6-v1[1m]",
	}
	result := mergeSettings(common, profile)

	if result["model"] != "us.anthropic.claude-opus-4-6-v1[1m]" {
		t.Errorf("expected profile model, got %v", result["model"])
	}
	if result["effortLevel"] != "high" {
		t.Errorf("expected common effortLevel preserved, got %v", result["effortLevel"])
	}
}

func TestNestedObjectMerge(t *testing.T) {
	common := map[string]any{
		"statusLine": map[string]any{
			"type":    "command",
			"command": "bash ~/.claude/statusline.sh",
		},
	}
	profile := map[string]any{
		"statusLine": map[string]any{
			"command": "bash ~/.claude/statusline-work.sh",
		},
	}
	result := mergeSettings(common, profile)

	sl := result["statusLine"].(map[string]any)
	if sl["type"] != "command" {
		t.Errorf("expected common type preserved, got %v", sl["type"])
	}
	if sl["command"] != "bash ~/.claude/statusline-work.sh" {
		t.Errorf("expected profile command, got %v", sl["command"])
	}
}

func TestArrayReplace(t *testing.T) {
	common := map[string]any{
		"someList": []any{"a", "b", "c"},
	}
	profile := map[string]any{
		"someList": []any{"x", "y"},
	}
	result := mergeSettings(common, profile)

	arr := result["someList"].([]any)
	if len(arr) != 2 || arr[0] != "x" || arr[1] != "y" {
		t.Errorf("expected profile array to replace, got %v", arr)
	}
}

func TestPermissionsAllowUnion(t *testing.T) {
	common := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read", "Write", "Edit"},
		},
	}
	profile := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Edit", "Grep", "WebFetch"},
		},
	}
	result := mergeSettings(common, profile)

	perms := result["permissions"].(map[string]any)
	allow := perms["allow"].([]any)

	// Should have union: Read, Write, Edit, Grep, WebFetch (no duplicate Edit)
	expected := map[string]bool{
		"Read": true, "Write": true, "Edit": true,
		"Grep": true, "WebFetch": true,
	}
	if len(allow) != len(expected) {
		t.Errorf("expected %d permissions, got %d: %v", len(expected), len(allow), allow)
	}
	for _, v := range allow {
		if !expected[v.(string)] {
			t.Errorf("unexpected permission: %v", v)
		}
	}
}

func TestEnabledPluginsObjectMerge(t *testing.T) {
	common := map[string]any{
		"enabledPlugins": map[string]any{
			"plugin-a@marketplace": true,
			"plugin-b@marketplace": true,
		},
	}
	profile := map[string]any{
		"enabledPlugins": map[string]any{
			"plugin-b@marketplace": false,
			"plugin-c@marketplace": true,
		},
	}
	result := mergeSettings(common, profile)

	plugins := result["enabledPlugins"].(map[string]any)
	if plugins["plugin-a@marketplace"] != true {
		t.Error("expected plugin-a preserved from common")
	}
	if plugins["plugin-b@marketplace"] != false {
		t.Error("expected plugin-b overridden by profile")
	}
	if plugins["plugin-c@marketplace"] != true {
		t.Error("expected plugin-c added from profile")
	}
}

func TestNullSentinelDeletion(t *testing.T) {
	common := map[string]any{
		"voiceEnabled": true,
		"effortLevel":  "high",
		"keepMe":       "yes",
	}
	profile := map[string]any{
		"voiceEnabled": nil, // delete this
	}
	result := mergeSettings(common, profile)

	if _, exists := result["voiceEnabled"]; exists {
		t.Error("expected voiceEnabled to be deleted by null sentinel")
	}
	if result["effortLevel"] != "high" {
		t.Error("expected effortLevel preserved")
	}
	if result["keepMe"] != "yes" {
		t.Error("expected keepMe preserved")
	}
}

func TestNestedNullSentinel(t *testing.T) {
	common := map[string]any{
		"env": map[string]any{
			"AWS_PROFILE": "default",
			"AWS_REGION":  "us-east-1",
			"KEEP_THIS":   "yes",
		},
	}
	profile := map[string]any{
		"env": map[string]any{
			"AWS_PROFILE": nil, // delete just this key
		},
	}
	result := mergeSettings(common, profile)

	env := result["env"].(map[string]any)
	if _, exists := env["AWS_PROFILE"]; exists {
		t.Error("expected AWS_PROFILE deleted")
	}
	if env["AWS_REGION"] != "us-east-1" {
		t.Error("expected AWS_REGION preserved")
	}
	if env["KEEP_THIS"] != "yes" {
		t.Error("expected KEEP_THIS preserved")
	}
}

func TestEmptyCommon(t *testing.T) {
	common := map[string]any{}
	profile := map[string]any{
		"model": "opus",
	}
	result := mergeSettings(common, profile)
	if result["model"] != "opus" {
		t.Errorf("expected profile value, got %v", result["model"])
	}
}

func TestEmptyProfile(t *testing.T) {
	common := map[string]any{
		"model": "sonnet",
	}
	profile := map[string]any{}
	result := mergeSettings(common, profile)
	if result["model"] != "sonnet" {
		t.Errorf("expected common value, got %v", result["model"])
	}
}

func TestBothEmpty(t *testing.T) {
	result := mergeSettings(map[string]any{}, map[string]any{})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestFullRealisticMerge(t *testing.T) {
	// Simulates merging Paul's actual personal (common) and work (profile) configs
	common := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read", "Write", "Edit", "Grep"},
		},
		"effortLevel": "high",
		"voiceEnabled": true,
		"enabledPlugins": map[string]any{
			"claude-code-setup@claude-plugins-official": true,
		},
	}
	profile := map[string]any{
		"env": map[string]any{
			"AWS_PROFILE":             "eak-claude",
			"AWS_REGION":              "us-east-1",
			"CLAUDE_CODE_USE_BEDROCK": "1",
		},
		"model": "us.anthropic.claude-opus-4-6-v1[1m]",
		"permissions": map[string]any{
			"allow": []any{"WebFetch", "WebSearch", "Bash(npm install)"},
		},
		"enabledPlugins": map[string]any{
			"frontend-design@claude-plugins-official": true,
			"gopls-lsp@claude-plugins-official":       true,
		},
	}

	result := mergeSettings(common, profile)

	// Model should be profile's
	if result["model"] != "us.anthropic.claude-opus-4-6-v1[1m]" {
		t.Errorf("model: got %v", result["model"])
	}

	// effortLevel from common preserved
	if result["effortLevel"] != "high" {
		t.Errorf("effortLevel: got %v", result["effortLevel"])
	}

	// voiceEnabled from common preserved
	if result["voiceEnabled"] != true {
		t.Errorf("voiceEnabled: got %v", result["voiceEnabled"])
	}

	// permissions.allow should be union of both = 7 items
	// Read, Write, Edit, Grep + WebFetch, WebSearch, Bash(npm install)
	perms := result["permissions"].(map[string]any)
	allow := perms["allow"].([]any)
	expectedPerms := map[string]bool{
		"Read": true, "Write": true, "Edit": true, "Grep": true,
		"WebFetch": true, "WebSearch": true, "Bash(npm install)": true,
	}
	if len(allow) != 7 {
		t.Errorf("expected 7 permissions, got %d: %v", len(allow), allow)
	}
	for _, v := range allow {
		if !expectedPerms[v.(string)] {
			t.Errorf("unexpected permission: %v", v)
		}
	}

	// enabledPlugins should have all three
	plugins := result["enabledPlugins"].(map[string]any)
	if len(plugins) != 3 {
		t.Errorf("expected 3 plugins, got %d: %v", len(plugins), plugins)
	}

	// env should exist from profile
	env := result["env"].(map[string]any)
	if env["AWS_PROFILE"] != "eak-claude" {
		t.Errorf("env.AWS_PROFILE: got %v", env["AWS_PROFILE"])
	}
}

func TestLoadJSON_FileNotExist(t *testing.T) {
	result, err := loadJSON(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestLoadJSON_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	content := []byte(`{"model": "opus", "nested": {"key": "val"}}`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	result, err := loadJSON(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["model"] != "opus" {
		t.Errorf("expected model=opus, got %v", result["model"])
	}
	nested := result["nested"].(map[string]any)
	if nested["key"] != "val" {
		t.Errorf("expected nested.key=val, got %v", nested["key"])
	}
}

func TestLoadJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{not valid`), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	_, err := loadJSON(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestWriteJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	data := map[string]any{
		"model": "opus",
		"nested": map[string]any{
			"key": "val",
		},
	}
	if err := writeJSON(path, data); err != nil {
		t.Fatalf("writeJSON failed: %v", err)
	}

	// Verify temp file is cleaned up (renamed away)
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("temp file should not exist after writeJSON")
	}

	// Read back and verify
	result, err := loadJSON(path)
	if err != nil {
		t.Fatalf("loadJSON failed: %v", err)
	}
	if result["model"] != "opus" {
		t.Errorf("expected model=opus, got %v", result["model"])
	}

	// Verify it's valid indented JSON with trailing newline
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if raw[len(raw)-1] != '\n' {
		t.Error("expected trailing newline")
	}
	var check map[string]any
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestNilInputs(t *testing.T) {
	// mergeSettings should handle nil maps gracefully
	// by treating them as empty maps via the make in mergeSettings
	result := mergeSettings(nil, nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil inputs, got %v", result)
	}
}

func TestDeepMergeDoesNotMutateInputs(t *testing.T) {
	// Test nested maps — the real bug was that common's nested objects
	// were aliased into result, then mutated by the profile merge pass.
	common := map[string]any{
		"model": "sonnet",
		"statusLine": map[string]any{
			"enabled": true,
			"format":  "default",
		},
		"env": map[string]any{
			"FOO": "bar",
		},
	}
	profile := map[string]any{
		"model": "opus",
		"statusLine": map[string]any{
			"enabled": false,
		},
		"env": map[string]any{
			"BAZ": "qux",
		},
	}

	// Snapshot common's nested maps before merge
	commonStatusEnabled := common["statusLine"].(map[string]any)["enabled"]
	commonStatusFormat := common["statusLine"].(map[string]any)["format"]
	commonEnvFoo := common["env"].(map[string]any)["FOO"]

	result := mergeSettings(common, profile)

	// Verify result is correct (profile overrides)
	statusLine := result["statusLine"].(map[string]any)
	if statusLine["enabled"] != false {
		t.Errorf("expected statusLine.enabled=false in result, got %v", statusLine["enabled"])
	}
	if statusLine["format"] != "default" {
		t.Errorf("expected statusLine.format='default' in result (inherited from common), got %v", statusLine["format"])
	}

	// Verify common was NOT mutated
	if common["model"] != "sonnet" {
		t.Error("mergeSettings mutated common['model']")
	}
	if common["statusLine"].(map[string]any)["enabled"] != commonStatusEnabled {
		t.Errorf("mergeSettings mutated common['statusLine']['enabled']: was %v, now %v",
			commonStatusEnabled, common["statusLine"].(map[string]any)["enabled"])
	}
	if common["statusLine"].(map[string]any)["format"] != commonStatusFormat {
		t.Error("mergeSettings mutated common['statusLine']['format']")
	}
	if common["env"].(map[string]any)["FOO"] != commonEnvFoo {
		t.Error("mergeSettings mutated common['env']['FOO']")
	}
	// common's env should NOT have profile's BAZ key
	if _, hasBaz := common["env"].(map[string]any)["BAZ"]; hasBaz {
		t.Error("mergeSettings leaked profile key 'BAZ' into common['env']")
	}
}

func TestPermissionsAllowUnionPreservesOrder(t *testing.T) {
	common := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Read", "Write"},
		},
	}
	profile := map[string]any{
		"permissions": map[string]any{
			"allow": []any{"Grep", "Read"},
		},
	}
	result := mergeSettings(common, profile)

	perms := result["permissions"].(map[string]any)
	allow := perms["allow"].([]any)

	// Order should be: common items first, then new profile items
	// Read, Write (from common), Grep (new from profile). "Read" is a duplicate, skipped.
	if len(allow) != 3 {
		t.Fatalf("expected 3 items, got %d: %v", len(allow), allow)
	}
	if allow[0] != "Read" || allow[1] != "Write" || allow[2] != "Grep" {
		t.Errorf("unexpected order: %v", allow)
	}
}

// ---------------------------------------------------------------------------
// Env file tests
// ---------------------------------------------------------------------------

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")

	content := `# AWS Bedrock config
AWS_PROFILE=bedrock
AWS_REGION=us-west-2

# API key with quotes
ANTHROPIC_API_KEY="sk-ant-test123"
SINGLE_QUOTED='hello world'

# Blank lines and comments are ignored
`
	if err := os.WriteFile(envPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	vars, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatalf("parseEnvFile: %v", err)
	}

	expected := map[string]string{
		"AWS_PROFILE":     "bedrock",
		"AWS_REGION":      "us-west-2",
		"ANTHROPIC_API_KEY": "sk-ant-test123",
		"SINGLE_QUOTED":   "hello world",
	}

	if len(vars) != len(expected) {
		t.Errorf("got %d vars, want %d", len(vars), len(expected))
	}
	for k, want := range expected {
		got, ok := vars[k]
		if !ok {
			t.Errorf("missing key %s", k)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestParseEnvFileNotExist(t *testing.T) {
	vars, err := parseEnvFile("/nonexistent/env")
	if err != nil {
		t.Fatalf("should return nil for nonexistent file: %v", err)
	}
	if vars != nil {
		t.Errorf("should return nil map, got %v", vars)
	}
}

func TestParseEnvFileInvalid(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")
	if err := os.WriteFile(envPath, []byte("no_equals_sign\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := parseEnvFile(envPath)
	if err == nil {
		t.Fatal("expected error for invalid line")
	}
	if !strings.Contains(err.Error(), "invalid line") {
		t.Errorf("expected 'invalid line' error, got: %v", err)
	}
}

func TestParseEnvFileInvalidKeyNames(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"valid key", "AWS_PROFILE=test\n", false},
		{"valid underscore prefix", "_PRIVATE=yes\n", false},
		{"valid lowercase", "myvar=val\n", false},
		{"starts with number", "1PASSWORD=token\n", true},
		{"contains space", "MY VAR=val\n", true},
		{"contains hyphen", "MY-VAR=val\n", true},
		{"empty key", "=value\n", true},
		{"contains dot", "MY.VAR=val\n", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			envPath := filepath.Join(dir, "env")
			if err := os.WriteFile(envPath, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := parseEnvFile(envPath)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for key in %q", tc.content)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error for key in %q: %v", tc.content, err)
			}
			if tc.wantErr && err != nil && !strings.Contains(err.Error(), "invalid env var name") {
				t.Errorf("expected 'invalid env var name' in error, got: %v", err)
			}
		})
	}
}

func TestWriteEnvFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")

	// Test values with special characters including shell-sensitive ones
	original := map[string]string{
		"SIMPLE":        "hello",
		"WITH_SPACES":   "hello world",
		"WITH_EQUALS":   "key=value=extra",
		"WITH_QUOTES":   `she said "hi"`,
		"WITH_SINGLE":   "it's fine",
		"WITH_BACK":     `path\to\file`,
		"EMPTY":         "",
		"BACKSLASH_QUO": `\"`,
		"DOLLAR":        "$HOME/bin",
		"BACKTICK":      "`whoami`",
		"NEWLINE":       "line1\nline2",
	}

	if err := writeEnvFile(envPath, original); err != nil {
		t.Fatalf("writeEnvFile: %v", err)
	}

	roundTripped, err := parseEnvFile(envPath)
	if err != nil {
		t.Fatalf("parseEnvFile: %v", err)
	}

	for k, want := range original {
		got, ok := roundTripped[k]
		if !ok {
			t.Errorf("missing key %s after round-trip", k)
			continue
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", k, got, want)
		}
	}
	if len(roundTripped) != len(original) {
		t.Errorf("got %d keys, want %d", len(roundTripped), len(original))
	}
}

func TestMergeEnvFiles(t *testing.T) {
	dir := t.TempDir()
	commonDir := filepath.Join(dir, "common")
	profileDir := filepath.Join(dir, "profile")
	genDir := filepath.Join(dir, "generated")
	for _, d := range []string{commonDir, profileDir, genDir} {
		os.MkdirAll(d, 0o755)
	}

	// Common env
	os.WriteFile(filepath.Join(commonDir, "env"), []byte("SHARED=common\nOVERRIDE=base\n"), 0o644)

	// Profile env overrides OVERRIDE
	os.WriteFile(filepath.Join(profileDir, "env"), []byte("OVERRIDE=profile\nPROFILE_ONLY=yes\n"), 0o644)

	if err := mergeEnvFiles(commonDir, profileDir, genDir); err != nil {
		t.Fatalf("mergeEnvFiles: %v", err)
	}

	// Read merged result
	vars, err := parseEnvFile(filepath.Join(genDir, "env"))
	if err != nil {
		t.Fatalf("reading merged env: %v", err)
	}

	if vars["SHARED"] != "common" {
		t.Errorf("SHARED = %q, want 'common'", vars["SHARED"])
	}
	if vars["OVERRIDE"] != "profile" {
		t.Errorf("OVERRIDE = %q, want 'profile' (profile should win)", vars["OVERRIDE"])
	}
	if vars["PROFILE_ONLY"] != "yes" {
		t.Errorf("PROFILE_ONLY = %q, want 'yes'", vars["PROFILE_ONLY"])
	}
}

func TestMergeEnvFilesEmpty(t *testing.T) {
	dir := t.TempDir()
	commonDir := filepath.Join(dir, "common")
	profileDir := filepath.Join(dir, "profile")
	genDir := filepath.Join(dir, "generated")
	for _, d := range []string{commonDir, profileDir, genDir} {
		os.MkdirAll(d, 0o755)
	}

	// No env files — should not create generated/env
	if err := mergeEnvFiles(commonDir, profileDir, genDir); err != nil {
		t.Fatalf("mergeEnvFiles: %v", err)
	}

	if _, err := os.Stat(filepath.Join(genDir, "env")); err == nil {
		t.Error("should not create env file when both sources are empty")
	}
}

func TestReadEnvExports(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "env"), []byte("AWS_PROFILE=bedrock\nAWS_REGION=us-west-2\n"), 0o644)

	exports, err := readEnvExports(dir)
	if err != nil {
		t.Fatalf("readEnvExports: %v", err)
	}
	if len(exports) != 2 {
		t.Fatalf("expected 2 exports, got %d", len(exports))
	}
	if exports[0] != "export AWS_PROFILE='bedrock'" {
		t.Errorf("export[0] = %q", exports[0])
	}
	if exports[1] != "export AWS_REGION='us-west-2'" {
		t.Errorf("export[1] = %q", exports[1])
	}
}

func TestReadEnvUnsets(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "env"), []byte("AWS_PROFILE=bedrock\nAWS_REGION=us-west-2\n"), 0o644)

	unsets, err := readEnvUnsets(dir)
	if err != nil {
		t.Fatalf("readEnvUnsets: %v", err)
	}
	if len(unsets) != 2 {
		t.Fatalf("expected 2 unsets, got %d", len(unsets))
	}
	if unsets[0] != "unset AWS_PROFILE" {
		t.Errorf("unset[0] = %q", unsets[0])
	}
	if unsets[1] != "unset AWS_REGION" {
		t.Errorf("unset[1] = %q", unsets[1])
	}
}

func TestUseOutputsEnvExports(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a profile
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "env-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Write an env file for the profile
	envPath := filepath.Join(dir, "profiles", "env-test", "env")
	if err := os.WriteFile(envPath, []byte("AWS_PROFILE=test-profile\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Regenerate to pick up the env file
	_, err = executeWithStdout(t, "--config-dir", dir, "regenerate", "env-test")
	if err != nil {
		t.Fatalf("regen failed: %v", err)
	}

	// Use the profile
	out, err := executeWithStdout(t, "--config-dir", dir, "use", "env-test")
	if err != nil {
		t.Fatalf("use failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "export AWS_PROFILE='test-profile'") {
		t.Errorf("expected AWS_PROFILE export in output:\n%s", out)
	}
}

func TestUseUnsetClearsEnvVars(t *testing.T) {
	dir := setupTestEnv(t)

	// Create and activate a profile with env vars
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "env-unset")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	envPath := filepath.Join(dir, "profiles", "env-unset", "env")
	os.WriteFile(envPath, []byte("MY_VAR=hello\n"), 0o644)
	executeWithStdout(t, "--config-dir", dir, "regenerate", "env-unset")
	executeWithStdout(t, "--config-dir", dir, "use", "env-unset")

	// Simulate the shell having APM_PROFILE set (use --unset reads from env)
	t.Setenv("APM_PROFILE", "env-unset")

	// Now unset
	out, err := executeWithStdout(t, "--config-dir", dir, "use", "--unset")
	if err != nil {
		t.Fatalf("use --unset failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "unset MY_VAR") {
		t.Errorf("expected 'unset MY_VAR' in output:\n%s", out)
	}
	if !strings.Contains(out, "unset APM_PROFILE") {
		t.Errorf("expected 'unset APM_PROFILE' in output:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Input hash tests
// ---------------------------------------------------------------------------

func setupInputHashTest(t *testing.T) *Config {
	t.Helper()
	cfg := newTestConfig(t)

	// Common settings
	if err := os.WriteFile(filepath.Join(cfg.CommonDir, "settings.json"),
		[]byte(`{"permissions":{"allow":["Read"]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Profile
	profDir := filepath.Join(cfg.ProfilesDir, "test")
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sub := range managedDirs {
		if err := os.MkdirAll(filepath.Join(profDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(profDir, "settings.json"),
		[]byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profDir, "profile.yaml"),
		[]byte("name: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return cfg
}

func TestComputeInputHash(t *testing.T) {
	cfg := setupInputHashTest(t)

	hash, err := computeInputHash(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 64 {
		t.Errorf("expected SHA-256 hash (64 hex chars), got length %d", len(hash))
	}

	// Same inputs = same hash
	hash2, err := computeInputHash(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if hash != hash2 {
		t.Errorf("expected deterministic hash, got %s vs %s", hash, hash2)
	}
}

func TestComputeInputHashChangesOnSettingsEdit(t *testing.T) {
	cfg := setupInputHashTest(t)

	hash1, err := computeInputHash(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}

	// Modify profile settings.json (change mtime)
	time.Sleep(10 * time.Millisecond) // ensure mtime differs
	settingsPath := filepath.Join(cfg.ProfileDir("test"), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"model":"sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	hash2, err := computeInputHash(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}

	if hash1 == hash2 {
		t.Error("expected different hash after settings change")
	}
}

func TestComputeInputHashChangesOnSkillAdd(t *testing.T) {
	cfg := setupInputHashTest(t)

	hash1, err := computeInputHash(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}

	// Add a skill to the profile
	skillPath := filepath.Join(cfg.ProfileDir("test"), "skills", "new-skill.md")
	if err := os.WriteFile(skillPath, []byte("# new skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash2, err := computeInputHash(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}

	if hash1 == hash2 {
		t.Error("expected different hash after adding a skill")
	}
}

func TestComputeInputHashChangesOnEnvEdit(t *testing.T) {
	cfg := setupInputHashTest(t)

	hash1, err := computeInputHash(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}

	// Add an env file
	envPath := filepath.Join(cfg.ProfileDir("test"), envFileName)
	if err := os.WriteFile(envPath, []byte("FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash2, err := computeInputHash(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}

	if hash1 == hash2 {
		t.Error("expected different hash after adding env file")
	}
}

func TestIsProfileStale(t *testing.T) {
	cfg := setupInputHashTest(t)

	// Generate to create the meta file with input_hash
	if err := generateProfile(cfg, "test"); err != nil {
		t.Fatal(err)
	}

	// Should not be stale right after generation
	stale, _, err := isProfileStale(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Error("expected profile to be fresh right after generation")
	}

	// Modify a source file
	time.Sleep(10 * time.Millisecond)
	settingsPath := filepath.Join(cfg.ProfileDir("test"), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"model":"haiku"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should be stale now
	stale, _, err = isProfileStale(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("expected profile to be stale after source change")
	}
}

func TestIsProfileStaleMissingMeta(t *testing.T) {
	cfg := setupInputHashTest(t)

	genDir := cfg.GeneratedProfileDir("test")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Missing meta file = stale
	stale, _, err := isProfileStale(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("expected stale when meta file is missing")
	}
}

func TestIsProfileStaleMissingInputHash(t *testing.T) {
	cfg := setupInputHashTest(t)

	genDir := cfg.GeneratedProfileDir("test")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write meta without input_hash (legacy format)
	meta := map[string]string{"settings_hash": "abc123"}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(genDir, apmMetaFile), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Missing input_hash = stale
	stale, _, err := isProfileStale(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("expected stale when input_hash is missing from meta")
	}
}

func TestEnsureFresh(t *testing.T) {
	cfg := setupInputHashTest(t)

	// ensureFresh should generate when dir missing
	if err := ensureFresh(cfg, "test"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("test")
	if _, err := os.Stat(filepath.Join(genDir, "settings.json")); err != nil {
		t.Error("settings.json should exist after ensureFresh")
	}

	// Should not regenerate when fresh (check meta is preserved)
	meta1, err := os.ReadFile(filepath.Join(genDir, apmMetaFile))
	if err != nil {
		t.Fatal(err)
	}

	if err := ensureFresh(cfg, "test"); err != nil {
		t.Fatal(err)
	}

	meta2, err := os.ReadFile(filepath.Join(genDir, apmMetaFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(meta1) != string(meta2) {
		t.Error("meta should not change when profile is fresh")
	}
}

func TestEnsureFreshRegeneratesOnChange(t *testing.T) {
	cfg := setupInputHashTest(t)

	// Generate first
	if err := generateProfile(cfg, "test"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("test")
	meta1, err := os.ReadFile(filepath.Join(genDir, apmMetaFile))
	if err != nil {
		t.Fatal(err)
	}

	// Modify source
	time.Sleep(10 * time.Millisecond)
	settingsPath := filepath.Join(cfg.ProfileDir("test"), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"model":"haiku"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// ensureFresh should regenerate
	if err := ensureFresh(cfg, "test"); err != nil {
		t.Fatal(err)
	}

	meta2, err := os.ReadFile(filepath.Join(genDir, apmMetaFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(meta1) == string(meta2) {
		t.Error("meta should change after regeneration due to staleness")
	}
}

func TestGenerateStoresInputHash(t *testing.T) {
	cfg := setupInputHashTest(t)

	if err := generateProfile(cfg, "test"); err != nil {
		t.Fatal(err)
	}

	genDir := cfg.GeneratedProfileDir("test")
	data, err := os.ReadFile(filepath.Join(genDir, apmMetaFile))
	if err != nil {
		t.Fatal(err)
	}

	var meta map[string]string
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}

	if hash, ok := meta[metaKeyInputHash]; !ok {
		t.Error("expected input_hash in meta")
	} else if len(hash) != 64 {
		t.Errorf("expected 64-char SHA-256 input_hash, got length %d", len(hash))
	}
}

// ---------------------------------------------------------------------------
// Generate tests
// ---------------------------------------------------------------------------

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

	hash, ok := meta[metaKeyInputHash]
	if !ok {
		t.Fatal("expected input_hash key in .apm-meta.json")
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

// ---------------------------------------------------------------------------
// Activate tests
// ---------------------------------------------------------------------------

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

func TestActivateEnsuresFresh(t *testing.T) {
	cfg, _ := setupActivateTest(t)

	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activateProfile failed: %v", err)
	}

	// activateProfile should NOT persist active_profile to config
	cf, err := cfg.readConfigFile()
	if err != nil {
		t.Fatalf("readConfigFile: %v", err)
	}
	if cf.ActiveProfile != "" {
		t.Errorf("active_profile should not be set in config, got %q", cf.ActiveProfile)
	}

	// Generated dir should exist
	genDir := cfg.GeneratedProfileDir("test-profile")
	if _, err := os.Stat(genDir); err != nil {
		t.Errorf("generated dir should exist: %v", err)
	}
}

func TestActivateSwitchProfile(t *testing.T) {
	cfg, _ := setupActivateTest(t)

	// Create second profile
	if err := createProfile(cfg, "other-profile", "", "other"); err != nil {
		t.Fatal(err)
	}

	// Activate first profile
	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activate first: %v", err)
	}

	// Switch to second profile
	if err := activateProfile(cfg, "other-profile"); err != nil {
		t.Fatalf("activate second: %v", err)
	}

	// Both generated dirs should exist
	for _, name := range []string{"test-profile", "other-profile"} {
		genDir := cfg.GeneratedProfileDir(name)
		if _, err := os.Stat(genDir); err != nil {
			t.Errorf("generated dir for %s should exist: %v", name, err)
		}
	}
}

func TestDeactivateIsNoOp(t *testing.T) {
	cfg, _ := setupActivateTest(t)

	// Activate
	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Deactivate is now a no-op (env var cleanup is done in shell)
	if err := deactivateProfile(cfg); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Generated dir should still exist
	genDir := cfg.GeneratedProfileDir("test-profile")
	if _, err := os.Stat(genDir); err != nil {
		t.Errorf("generated dir should still exist: %v", err)
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

	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activateProfile failed: %v", err)
	}

	// Generated dir should exist
	genDir := cfg.GeneratedProfileDir("test-profile")
	if _, err := os.Stat(genDir); err != nil {
		t.Errorf("generated dir should exist: %v", err)
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

func TestDeactivatePreservesGeneratedDirs(t *testing.T) {
	cfg, _ := setupActivateTest(t)

	// Activate
	if err := activateProfile(cfg, "test-profile"); err != nil {
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

func TestCopyClaudeJSON(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create ~/.claude.json
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	content := `{"oauthTokens":{"prod":"token123"}}`
	if err := os.WriteFile(claudeJSON, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Copy to a generated dir
	genDir := filepath.Join(fakeHome, "generated", "test")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyClaudeJSON(genDir); err != nil {
		t.Fatalf("copyClaudeJSON: %v", err)
	}

	// Verify copied file
	data, err := os.ReadFile(filepath.Join(genDir, ".claude.json"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(data) != content {
		t.Errorf("copied content = %q, want %q", string(data), content)
	}
}

func TestCopyClaudeJSONNoFile(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	genDir := filepath.Join(fakeHome, "generated", "test")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// No ~/.claude.json — should succeed gracefully
	if err := copyClaudeJSON(genDir); err != nil {
		t.Fatalf("should succeed when no ~/.claude.json exists: %v", err)
	}
}

func TestActivateDeactivateRoundtrip(t *testing.T) {
	cfg, _ := setupActivateTest(t)

	// Activate
	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Verify generated dir has content
	genDir := cfg.GeneratedProfileDir("test-profile")
	entries, err := os.ReadDir(genDir)
	if err != nil {
		t.Fatalf("readdir generated: %v", err)
	}
	if len(entries) == 0 {
		t.Error("generated dir should have content")
	}

	// Deactivate
	if err := deactivateProfile(cfg); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Re-activate (should work again)
	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("re-activate: %v", err)
	}

	// Generated dir should still exist after re-activation
	if _, err := os.Stat(genDir); err != nil {
		t.Errorf("generated dir should exist after re-activation: %v", err)
	}
}

func TestNukePreservesCurrentState(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)

	// Create ~/.claude.json with original auth
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"auth":"original"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate active profile via env var (nukeAPM checks APM_PROFILE)
	t.Setenv("APM_PROFILE", "test-profile")

	// Activate (ensures generated dir exists)
	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Copy auth into generated dir (simulating what happens with CLAUDE_CONFIG_DIR)
	genDir := cfg.GeneratedProfileDir("test-profile")
	if err := os.WriteFile(filepath.Join(genDir, ".claude.json"), []byte(`{"auth":"updated-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "runtime-state.json"), []byte(`{"state":"active"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Remove ~/.claude to simulate the new model (no symlink, just CLAUDE_CONFIG_DIR)
	os.RemoveAll(filepath.Join(fakeHome, ".claude"))

	// Nuke
	if err := nukeAPM(cfg); err != nil {
		t.Fatalf("nukeAPM: %v", err)
	}

	// ~/.claude should be created from flattened generated dir
	claudePath := filepath.Join(fakeHome, ".claude")
	fi, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf("~/.claude should exist after nuke: %v", err)
	}
	if isSymlink(fi) {
		t.Error("~/.claude should be a real directory, not a symlink")
	}
	if !fi.IsDir() {
		t.Error("~/.claude should be a directory")
	}

	// ~/.claude.json should have the UPDATED token (from generated dir)
	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatalf("~/.claude.json should exist: %v", err)
	}
	if string(data) != `{"auth":"updated-token"}` {
		t.Errorf("~/.claude.json should preserve current auth, got %q", string(data))
	}

	// Runtime state should be preserved in flattened ~/.claude
	data, err = os.ReadFile(filepath.Join(claudePath, "runtime-state.json"))
	if err != nil {
		t.Fatalf("runtime state should be preserved: %v", err)
	}
	if string(data) != `{"state":"active"}` {
		t.Errorf("runtime state content wrong: got %q", string(data))
	}

	// .apm-meta.json should be removed (APM-specific artifact)
	if _, err := os.Stat(filepath.Join(claudePath, ".apm-meta.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error(".apm-meta.json should be removed after nuke")
	}

	// .claude.json should be removed from flattened dir (it's at ~/.claude.json)
	if _, err := os.Stat(filepath.Join(claudePath, ".claude.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error(".claude.json should be removed from flattened ~/.claude")
	}

	// Profiles and generated dirs should be gone
	if _, err := os.Stat(cfg.ProfilesDir); !errors.Is(err, os.ErrNotExist) {
		t.Error("profiles directory should be removed after nuke")
	}
	if _, err := os.Stat(cfg.GeneratedDir); !errors.Is(err, os.ErrNotExist) {
		t.Error("generated directory should be removed after nuke")
	}

	// Common directory should still exist
	if _, err := os.Stat(cfg.CommonDir); err != nil {
		t.Error("common directory should be preserved after nuke")
	}

	// APM directory should still exist
	if _, err := os.Stat(cfg.APMDir); err != nil {
		t.Error("APM directory should still exist after nuke")
	}
}

func TestNukeResolvesSymlinks(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)
	t.Setenv("APM_PROFILE", "test-profile")

	// Create a skill file in the profile
	profileSkillsDir := filepath.Join(cfg.ProfilesDir, "test-profile", "skills")
	if err := os.MkdirAll(profileSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profileSkillsDir, "my-skill.md"), []byte("skill content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Activate (generates profile with skill symlinks)
	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Remove ~/.claude to simulate the new model
	os.RemoveAll(filepath.Join(fakeHome, ".claude"))

	// Verify the generated dir has the skill (as a symlink)
	genDir := cfg.GeneratedProfileDir("test-profile")
	genSkill := filepath.Join(genDir, "skills", "my-skill.md")
	fi, err := os.Lstat(genSkill)
	if err != nil {
		t.Fatalf("generated skill should exist: %v", err)
	}
	if !isSymlink(fi) {
		t.Fatalf("generated skill should be a symlink")
	}

	// Nuke
	if err := nukeAPM(cfg); err != nil {
		t.Fatalf("nukeAPM: %v", err)
	}

	// Skills should exist as real files (not symlinks) in ~/.claude/skills/
	claudePath := filepath.Join(fakeHome, ".claude")
	skillPath := filepath.Join(claudePath, "skills", "my-skill.md")
	fi, err = os.Lstat(skillPath)
	if err != nil {
		t.Fatalf("skill should exist after nuke: %v", err)
	}
	if isSymlink(fi) {
		t.Error("skill should be a real file after nuke, not a symlink")
	}
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "skill content" {
		t.Errorf("skill content wrong: got %q", string(data))
	}
}

func TestNukePreservesExternalSymlinkTargets(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)
	t.Setenv("APM_PROFILE", "test-profile")

	// Create an external directory (outside APM) with a skill file
	externalDir := filepath.Join(fakeHome, "my-skills-repo")
	if err := os.MkdirAll(externalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(externalDir, "external-skill.md"), []byte("external skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Symlink the external skill into the profile's skills dir
	profileSkillsDir := filepath.Join(cfg.ProfilesDir, "test-profile", "skills")
	if err := os.MkdirAll(profileSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(externalDir, "external-skill.md"),
		filepath.Join(profileSkillsDir, "external-skill.md"),
	); err != nil {
		t.Fatal(err)
	}

	// Activate
	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Nuke
	if err := nukeAPM(cfg); err != nil {
		t.Fatalf("nukeAPM: %v", err)
	}

	// The external skill file must still exist — nuke must not delete it
	data, err := os.ReadFile(filepath.Join(externalDir, "external-skill.md"))
	if err != nil {
		t.Fatalf("external skill file was deleted by nuke: %v", err)
	}
	if string(data) != "external skill" {
		t.Errorf("external skill content changed: got %q", string(data))
	}
}

func TestNukeNoActiveProfile(t *testing.T) {
	cfg, fakeHome := setupActivateTest(t)
	claudePath := filepath.Join(fakeHome, ".claude")

	// Don't activate any profile — ~/.claude is a real directory

	// Nuke should succeed
	if err := nukeAPM(cfg); err != nil {
		t.Fatalf("nukeAPM should succeed: %v", err)
	}

	// ~/.claude should still be a real directory with original content
	fi, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf("~/.claude should still exist: %v", err)
	}
	if isSymlink(fi) {
		t.Error("~/.claude should remain a real directory")
	}
	data, err := os.ReadFile(filepath.Join(claudePath, "some-file.txt"))
	if err != nil {
		t.Fatalf("original content should be preserved: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("original content wrong: got %q", string(data))
	}

	// Profiles and generated dirs should be gone
	if _, err := os.Stat(cfg.ProfilesDir); !errors.Is(err, os.ErrNotExist) {
		t.Error("profiles directory should be removed after nuke")
	}
	if _, err := os.Stat(cfg.GeneratedDir); !errors.Is(err, os.ErrNotExist) {
		t.Error("generated directory should be removed after nuke")
	}

	// Common directory should still exist
	if _, err := os.Stat(cfg.CommonDir); err != nil {
		t.Error("common directory should be preserved after nuke")
	}
}

func TestMigrateFromSymlinks(t *testing.T) {
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

	// Create a profile with legacy external state
	if err := createProfile(cfg, "legacy-prof", "", "legacy"); err != nil {
		t.Fatal(err)
	}
	extDir := filepath.Join(cfg.ProfilesDir, "legacy-prof", "external")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "claude.json"), []byte(`{"auth":"legacy"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create legacy backup dirs
	if err := os.MkdirAll(filepath.Join(apmDir, "claude-home"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apmDir, "claude-home", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(apmDir, "claude-home-external"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create a symlink at ~/.claude → some target (simulating old activation)
	claudePath := filepath.Join(fakeHome, ".claude")
	os.RemoveAll(claudePath)
	genDir := cfg.GeneratedProfileDir("legacy-prof")
	os.MkdirAll(genDir, 0o755)
	if err := os.Symlink(genDir, claudePath); err != nil {
		t.Fatal(err)
	}

	// Run migration
	if err := migrateFromSymlinks(cfg); err != nil {
		t.Fatalf("migrateFromSymlinks: %v", err)
	}

	// 1. ~/.claude symlink should be removed and backup restored
	fi, err := os.Lstat(claudePath)
	if err != nil {
		t.Fatalf("~/.claude should exist: %v", err)
	}
	if isSymlink(fi) {
		t.Error("~/.claude should not be a symlink after migration")
	}
	// Backup content should be restored
	if _, err := os.Stat(filepath.Join(claudePath, "settings.json")); err != nil {
		t.Error("backup content should be restored to ~/.claude")
	}

	// 2. External state should be migrated to generated dir
	data, err := os.ReadFile(filepath.Join(genDir, ".claude.json"))
	if err != nil {
		t.Fatalf("generated .claude.json should exist: %v", err)
	}
	if string(data) != `{"auth":"legacy"}` {
		t.Errorf("migrated .claude.json = %q, want legacy auth", string(data))
	}

	// 3. Legacy dirs should be cleaned up
	if _, err := os.Stat(filepath.Join(apmDir, "claude-home")); !errors.Is(err, os.ErrNotExist) {
		t.Error("claude-home should be removed after migration")
	}
	if _, err := os.Stat(filepath.Join(apmDir, "claude-home-external")); !errors.Is(err, os.ErrNotExist) {
		t.Error("claude-home-external should be removed after migration")
	}
	if _, err := os.Stat(extDir); !errors.Is(err, os.ErrNotExist) {
		t.Error("profiles/*/external should be removed after migration")
	}

	// 4. Migration marker should exist
	if _, err := os.Stat(filepath.Join(apmDir, ".migrated-v2")); err != nil {
		t.Error("migration marker should exist")
	}

	// 5. Running migration again should be a no-op
	if err := migrateFromSymlinks(cfg); err != nil {
		t.Fatalf("second migration should be a no-op: %v", err)
	}
}

// Fix 1: Verify nuke uses cfg.ClaudeDir, not defaultClaudeDir()
func TestNukeUsesConfiguredClaudeDir(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	apmDir := filepath.Join(fakeHome, ".config", "apm")
	customClaude := filepath.Join(fakeHome, "custom-claude")

	cfg := &Config{
		APMDir:       apmDir,
		ClaudeDir:    customClaude,
		CommonDir:    filepath.Join(apmDir, "common"),
		ProfilesDir:  filepath.Join(apmDir, "profiles"),
		GeneratedDir: filepath.Join(apmDir, "generated"),
		ConfigPath:   filepath.Join(apmDir, "config.yaml"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Create and activate a profile
	if err := createProfile(cfg, "test", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := activateProfile(cfg, "test"); err != nil {
		t.Fatal(err)
	}

	// Simulate active profile for nuke
	t.Setenv("APM_PROFILE", "test")

	// Add content to the generated dir
	genDir := cfg.GeneratedProfileDir("test")
	if err := os.WriteFile(filepath.Join(genDir, "runtime.json"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Nuke should flatten to customClaude, NOT ~/.claude
	if err := nukeAPM(cfg); err != nil {
		t.Fatalf("nukeAPM: %v", err)
	}

	// customClaude should have the flattened content
	if _, err := os.Stat(filepath.Join(customClaude, "runtime.json")); err != nil {
		t.Errorf("expected flattened content in custom claude dir: %v", err)
	}

	// Default ~/.claude should NOT have been created
	defaultClaude := filepath.Join(fakeHome, ".claude")
	if _, err := os.Stat(filepath.Join(defaultClaude, "runtime.json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("nuke should flatten to cfg.ClaudeDir, not default ~/.claude")
	}
}

// Fix 2: Verify copyDirFlat follows symlinked directories
func TestCopyDirFlatFollowsSymlinkedDirs(t *testing.T) {
	tmp := t.TempDir()

	// Create a real directory with files
	realDir := filepath.Join(tmp, "real-subdir")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create source dir with a symlink to the real directory
	srcDir := filepath.Join(tmp, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(srcDir, "linked-dir")); err != nil {
		t.Fatal(err)
	}

	// Flatten
	dstDir := filepath.Join(tmp, "dst")
	if err := copyDirFlat(srcDir, dstDir); err != nil {
		t.Fatalf("copyDirFlat: %v", err)
	}

	// The file inside the symlinked dir should exist in the output
	data, err := os.ReadFile(filepath.Join(dstDir, "linked-dir", "file.txt"))
	if err != nil {
		t.Fatalf("file inside symlinked dir not copied: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("content wrong: %q", string(data))
	}

	// The output should be a real directory, not a symlink
	fi, err := os.Lstat(filepath.Join(dstDir, "linked-dir"))
	if err != nil {
		t.Fatal(err)
	}
	if isSymlink(fi) {
		t.Error("expected real directory, got symlink")
	}
}

// Fix 5: Verify apm use does NOT persist active_profile
func TestUseDoesNotPersistActiveProfile(t *testing.T) {
	cfg, _ := setupActivateTest(t)

	// Activate a profile
	if err := activateProfile(cfg, "test-profile"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	// Config should NOT have active_profile set
	cf, err := cfg.readConfigFile()
	if err != nil {
		t.Fatalf("readConfigFile: %v", err)
	}
	if cf.ActiveProfile != "" {
		t.Errorf("active_profile should not be persisted to config, got %q", cf.ActiveProfile)
	}
}
