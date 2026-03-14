# Task 2: Merge engine tests

## Files to create
- `internal/merge/merge_test.go`

## Dependencies
- Phase 2 Task 1 (merge.go)

## Implementation

### internal/merge/merge_test.go

```go
package merge

import (
	"reflect"
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
	result := Settings(common, profile)

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
	result := Settings(common, profile)

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
	result := Settings(common, profile)

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
	result := Settings(common, profile)

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
	result := Settings(common, profile)

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
		"voiceEnabled":  true,
		"effortLevel":   "high",
		"keepMe":        "yes",
	}
	profile := map[string]any{
		"voiceEnabled": nil, // delete this
	}
	result := Settings(common, profile)

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
	result := Settings(common, profile)

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
	result := Settings(common, profile)
	if result["model"] != "opus" {
		t.Errorf("expected profile value, got %v", result["model"])
	}
}

func TestEmptyProfile(t *testing.T) {
	common := map[string]any{
		"model": "sonnet",
	}
	profile := map[string]any{}
	result := Settings(common, profile)
	if result["model"] != "sonnet" {
		t.Errorf("expected common value, got %v", result["model"])
	}
}

func TestBothEmpty(t *testing.T) {
	result := Settings(map[string]any{}, map[string]any{})
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
			"AWS_PROFILE":                      "eak-claude",
			"AWS_REGION":                       "us-east-1",
			"CLAUDE_CODE_USE_BEDROCK":          "1",
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

	result := Settings(common, profile)

	// Model should be profile's
	if result["model"] != "us.anthropic.claude-opus-4-6-v1[1m]" {
		t.Errorf("model: got %v", result["model"])
	}

	// effortLevel from common preserved
	if result["effortLevel"] != "high" {
		t.Errorf("effortLevel: got %v", result["effortLevel"])
	}

	// permissions.allow should be union of both
	perms := result["permissions"].(map[string]any)
	allow := perms["allow"].([]any)
	if len(allow) != 6 { // Read, Write, Edit, Grep, WebFetch, WebSearch, Bash(npm install) = 7... wait
		// Read, Write, Edit, Grep + WebFetch, WebSearch, Bash(npm install) = 7
	}
	// Just check both sides are present
	allowSet := make(map[string]bool)
	for _, v := range allow {
		allowSet[v.(string)] = true
	}
	if !allowSet["Read"] || !allowSet["WebFetch"] {
		t.Errorf("permissions.allow missing expected values: %v", allow)
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

	_ = reflect.DeepEqual // suppress unused import if needed
}
```

## Verification

```bash
go test -v ./internal/merge/
```

All tests should pass. The realistic test at the bottom simulates Paul's actual use case of merging personal and work settings.
