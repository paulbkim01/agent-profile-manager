package validate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileName(t *testing.T) {
	valid := []string{
		"work",
		"personal",
		"my-work",
		"dev-2",
		"a",
		"test-profile-123",
	}
	for _, name := range valid {
		if err := ProfileName(name); err != nil {
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
		{"default", "reserved name"},
		{"current", "reserved name"},
	}
	for _, tc := range invalid {
		if err := ProfileName(tc.name); err == nil {
			t.Errorf("expected %q (%s) to be invalid", tc.name, tc.desc)
		}
	}

	// Length limit
	long := ""
	for i := 0; i < 51; i++ {
		long += "a"
	}
	if err := ProfileName(long); err == nil {
		t.Errorf("expected 51-char name to be invalid")
	}
	if err := ProfileName(long[:50]); err != nil {
		t.Errorf("expected 50-char name to be valid, got: %v", err)
	}
}

func TestSettingsJSON(t *testing.T) {
	// Valid
	valid := filepath.Join(t.TempDir(), "ok.json")
	os.WriteFile(valid, []byte(`{"model":"opus"}`), 0o644)
	if err := SettingsJSON(valid); err != nil {
		t.Errorf("expected valid: %v", err)
	}

	// Empty object is valid
	empty := filepath.Join(t.TempDir(), "empty.json")
	os.WriteFile(empty, []byte(`{}`), 0o644)
	if err := SettingsJSON(empty); err != nil {
		t.Errorf("expected valid: %v", err)
	}

	// Invalid JSON
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte(`{broken`), 0o644)
	if err := SettingsJSON(bad); err == nil {
		t.Error("expected error for invalid JSON")
	}

	// Array (not an object)
	arr := filepath.Join(t.TempDir(), "arr.json")
	os.WriteFile(arr, []byte(`[1,2,3]`), 0o644)
	if err := SettingsJSON(arr); err == nil {
		t.Error("expected error for array")
	}

	// Null
	nullFile := filepath.Join(t.TempDir(), "null.json")
	os.WriteFile(nullFile, []byte(`null`), 0o644)
	if err := SettingsJSON(nullFile); err == nil {
		t.Error("expected error for null")
	}

	// Scalar string
	str := filepath.Join(t.TempDir(), "str.json")
	os.WriteFile(str, []byte(`"hello"`), 0o644)
	if err := SettingsJSON(str); err == nil {
		t.Error("expected error for string scalar")
	}

	// Missing file is valid (treated as empty {})
	if err := SettingsJSON("/nonexistent"); err != nil {
		t.Errorf("expected missing file to be valid, got: %v", err)
	}
}
