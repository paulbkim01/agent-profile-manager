package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
