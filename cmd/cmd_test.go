package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestEnv creates a temp directory structure for testing.
// Returns the config dir path and a cleanup function.
func setupTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create the common dir with empty settings.json
	commonDir := filepath.Join(dir, "common")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"skills", "commands", "agents"} {
		if err := os.MkdirAll(filepath.Join(commonDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(commonDir, "settings.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create profiles and generated dirs
	if err := os.MkdirAll(filepath.Join(dir, "profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "generated"), 0o755); err != nil {
		t.Fatal(err)
	}

	return dir
}

// resetFlags resets all package-level flag variables to their defaults.
// Cobra doesn't reset flag values between test runs, so tests that
// set --all, --from, --force etc. can leak into subsequent tests.
func resetFlags() {
	createFrom = ""
	createDesc = ""
	deleteForce = false
	regenAll = false
	useGlobal = false
	debug = false
	configDir = ""
}

// executeWithStdout runs the root command and captures real stdout.
// Some commands (like create, ls) use fmt.Printf which writes to os.Stdout,
// not cmd.OutOrStdout(), so we need to capture os.Stdout directly.
func executeWithStdout(t *testing.T, args ...string) (string, error) {
	t.Helper()
	resetFlags()

	// Save and restore stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	rootCmd.SetArgs(args)
	execErr := rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return buf.String(), execErr
}

func TestCreateAndList(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a profile
	out, err := executeWithStdout(t, "--config-dir", dir, "create", "test-profile", "--description", "a test profile")
	if err != nil {
		t.Fatalf("create failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Created profile 'test-profile'") {
		t.Errorf("unexpected create output: %s", out)
	}
	// Edit hint now goes to stderr, so it won't appear in captured stdout.
	// Just verify the main "Created" message is on stdout.

	// Verify profile dir exists
	profileDir := filepath.Join(dir, "profiles", "test-profile")
	if _, err := os.Stat(profileDir); err != nil {
		t.Fatalf("profile dir not created: %v", err)
	}

	// List profiles
	out, err = executeWithStdout(t, "--config-dir", dir, "ls")
	if err != nil {
		t.Fatalf("ls failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "test-profile") {
		t.Errorf("expected test-profile in ls output: %s", out)
	}
	if !strings.Contains(out, "a test profile") {
		t.Errorf("expected description in ls output: %s", out)
	}
}

func TestCreateDuplicate(t *testing.T) {
	dir := setupTestEnv(t)

	// Create first
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "dup")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// Create duplicate should fail
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "dup")
	if err == nil {
		t.Fatal("expected error creating duplicate profile")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestCreateInvalidName(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "INVALID")
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
}

func TestDescribe(t *testing.T) {
	dir := setupTestEnv(t)

	// Create profile first
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "desc-test", "--description", "describe me")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Describe it
	out, err := executeWithStdout(t, "--config-dir", dir, "describe", "desc-test")
	if err != nil {
		t.Fatalf("describe failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Profile: desc-test") {
		t.Errorf("expected profile name in describe output: %s", out)
	}
	if !strings.Contains(out, "Description: describe me") {
		t.Errorf("expected description in describe output: %s", out)
	}
	if !strings.Contains(out, "Created:") {
		t.Errorf("expected created timestamp in describe output: %s", out)
	}
}

func TestDescribeNotFound(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "describe", "nonexistent")
	if err == nil {
		t.Fatal("expected error describing nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestDeleteProfile(t *testing.T) {
	dir := setupTestEnv(t)

	// Create then delete
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "to-delete")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	out, err := executeWithStdout(t, "--config-dir", dir, "delete", "to-delete")
	if err != nil {
		t.Fatalf("delete failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Deleted profile 'to-delete'") {
		t.Errorf("unexpected delete output: %s", out)
	}

	// Verify it's gone
	profileDir := filepath.Join(dir, "profiles", "to-delete")
	if _, err := os.Stat(profileDir); !errors.Is(err, os.ErrNotExist) {
		t.Error("profile dir should have been removed")
	}
}

func TestDeleteNotFound(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "delete", "ghost")
	if err == nil {
		t.Fatal("expected error deleting nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestDeleteAlias(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "rm-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Use the "rm" alias
	out, err := executeWithStdout(t, "--config-dir", dir, "rm", "rm-test")
	if err != nil {
		t.Fatalf("rm alias failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Deleted profile 'rm-test'") {
		t.Errorf("unexpected rm output: %s", out)
	}
}

func TestRegenerateSingle(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a profile
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "regen-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Regenerate it
	out, err := executeWithStdout(t, "--config-dir", dir, "regenerate", "regen-test")
	if err != nil {
		t.Fatalf("regenerate failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Regenerated 'regen-test'") {
		t.Errorf("unexpected regenerate output: %s", out)
	}

	// Check generated dir exists
	genDir := filepath.Join(dir, "generated", "regen-test")
	if _, err := os.Stat(genDir); err != nil {
		t.Errorf("generated dir not created: %v", err)
	}
}

func TestRegenerateAll(t *testing.T) {
	dir := setupTestEnv(t)

	// Create two profiles
	for _, name := range []string{"prof-a", "prof-b"} {
		_, err := executeWithStdout(t, "--config-dir", dir, "create", name)
		if err != nil {
			t.Fatalf("create %s failed: %v", name, err)
		}
	}

	// Regenerate all
	out, err := executeWithStdout(t, "--config-dir", dir, "regenerate", "--all")
	if err != nil {
		t.Fatalf("regenerate --all failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Regenerated 'prof-a'") {
		t.Errorf("expected prof-a in output: %s", out)
	}
	if !strings.Contains(out, "Regenerated 'prof-b'") {
		t.Errorf("expected prof-b in output: %s", out)
	}
}

func TestRegenerateNoArgs(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "regenerate")
	if err == nil {
		t.Fatal("expected error with no args and no --all")
	}
	if !strings.Contains(err.Error(), "profile name required") {
		t.Errorf("expected error containing 'profile name required', got: %v", err)
	}
}

func TestRegenerateAlias(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "alias-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Use the "regen" alias
	out, err := executeWithStdout(t, "--config-dir", dir, "regen", "alias-test")
	if err != nil {
		t.Fatalf("regen alias failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Regenerated 'alias-test'") {
		t.Errorf("unexpected regen output: %s", out)
	}
}

func TestListEmpty(t *testing.T) {
	dir := setupTestEnv(t)

	out, err := executeWithStdout(t, "--config-dir", dir, "ls")
	if err != nil {
		t.Fatalf("ls failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No profiles") {
		t.Errorf("expected 'No profiles' message: %s", out)
	}
}

func TestListAlias(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "list-alias")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Use the "list" alias
	out, err := executeWithStdout(t, "--config-dir", dir, "list")
	if err != nil {
		t.Fatalf("list alias failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "list-alias") {
		t.Errorf("expected list-alias in output: %s", out)
	}
}

func TestCreateFromProfile(t *testing.T) {
	dir := setupTestEnv(t)

	// Create source profile
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "source-prof")
	if err != nil {
		t.Fatalf("create source failed: %v", err)
	}

	// Write some settings to source
	settingsPath := filepath.Join(dir, "profiles", "source-prof", "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"model": "opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create from source
	out, err := executeWithStdout(t, "--config-dir", dir, "create", "derived-prof", "--from", "source-prof")
	if err != nil {
		t.Fatalf("create --from failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Created profile 'derived-prof'") {
		t.Errorf("unexpected output: %s", out)
	}
	// Should not show "Edit it with" when --from is used
	if strings.Contains(out, "Edit it with") {
		t.Errorf("should not show edit hint when --from is used: %s", out)
	}

	// Verify settings were copied
	derivedSettings := filepath.Join(dir, "profiles", "derived-prof", "settings.json")
	data, err := os.ReadFile(derivedSettings)
	if err != nil {
		t.Fatalf("reading derived settings: %v", err)
	}
	if !strings.Contains(string(data), "opus") {
		t.Errorf("expected copied settings, got: %s", string(data))
	}
}
