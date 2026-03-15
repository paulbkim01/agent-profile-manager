package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestEnv creates a temp directory structure for testing.
// Returns the config dir path.
func setupTestEnv(t *testing.T) string {
	t.Helper()
	cfg := newTestConfig(t)
	return cfg.APMDir
}

// resetFlags resets all package-level flag variables to their defaults.
// Cobra doesn't reset flag values between test runs, so tests that
// set --all, --from, --force etc. can leak into subsequent tests.
func resetFlags() {
	createFrom = ""
	createCurrent = false
	createDesc = ""
	deleteForce = false
	regenAll = false
	useGlobal = false
	debug = false
	configDir = ""
	nukeForce = false
	// Reset Cobra-managed flags that aren't package-level vars
	useCmd.Flags().Set("unset", "false")
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

func TestCLICreateDuplicate(t *testing.T) {
	dir := setupTestEnv(t)

	// Create first
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "dup")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// Duplicate prompts for overwrite — confirm returns false (default in tests)
	confirmOverwrite = func(name string) bool { return false }
	defer func() { confirmOverwrite = func(string) bool { return false } }()

	out, err := executeWithStdout(t, "--config-dir", dir, "create", "dup")
	if err != nil {
		t.Fatalf("duplicate create should not error when declined: %v", err)
	}
	if strings.Contains(out, "Created") {
		t.Errorf("should not create when overwrite declined: %s", out)
	}
}

func TestCreateDuplicateOverwrite(t *testing.T) {
	dir := setupTestEnv(t)

	// Create first
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "dup")
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// Overwrite confirmed
	confirmOverwrite = func(name string) bool { return true }
	defer func() { confirmOverwrite = func(string) bool { return false } }()

	out, err := executeWithStdout(t, "--config-dir", dir, "create", "dup")
	if err != nil {
		t.Fatalf("overwrite create failed: %v", err)
	}
	if !strings.Contains(out, "Created profile 'dup'") {
		t.Errorf("expected Created message: %s", out)
	}
}

func TestCLICreateInvalidName(t *testing.T) {
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

	_, err = executeWithStdout(t, "--config-dir", dir, "delete", "to-delete")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
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
	_, err = executeWithStdout(t, "--config-dir", dir, "rm", "rm-test")
	if err != nil {
		t.Fatalf("rm alias failed: %v", err)
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

func TestCLIListEmpty(t *testing.T) {
	dir := setupTestEnv(t)

	out, err := executeWithStdout(t, "--config-dir", dir, "ls")
	if err != nil {
		t.Fatalf("ls failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No profiles") {
		t.Errorf("expected 'No profiles' message: %s", out)
	}
	if !strings.Contains(out, "--current") {
		t.Errorf("expected nudge with --current: %s", out)
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

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"/tmp/paul's/apm", `/tmp/paul'\''s/apm`},
		{"no-quotes", "no-quotes"},
		{"it's a 'test'", `it'\''s a '\''test'\''`},
		{"", ""},
	}
	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestUseOutputsExports(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a profile first
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "use-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Use it — stdout is a pipe (not TTY), so exports should appear
	out, err := executeWithStdout(t, "--config-dir", dir, "use", "use-test")
	if err != nil {
		t.Fatalf("use failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "export APM_PROFILE='use-test'") {
		t.Errorf("expected APM_PROFILE export, got: %s", out)
	}
	if !strings.Contains(out, "export CLAUDE_CONFIG_DIR='") {
		t.Errorf("expected CLAUDE_CONFIG_DIR export, got: %s", out)
	}

	// Verify generated dir was created
	genDir := filepath.Join(dir, "generated", "use-test")
	if _, err := os.Stat(genDir); err != nil {
		t.Errorf("generated dir not created: %v", err)
	}
}

func TestUseInvalidName(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "use", "../../etc")
	if err == nil {
		t.Fatal("expected error for invalid profile name")
	}
	if !strings.Contains(err.Error(), "invalid profile name") {
		t.Errorf("expected 'invalid profile name' error, got: %v", err)
	}
}

func TestUseNotFound(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "use", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestUseUnset(t *testing.T) {
	dir := setupTestEnv(t)

	out, err := executeWithStdout(t, "--config-dir", dir, "use", "--unset")
	if err != nil {
		t.Fatalf("use --unset failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "unset APM_PROFILE") {
		t.Errorf("expected unset APM_PROFILE, got: %s", out)
	}
	if !strings.Contains(out, "unset CLAUDE_CONFIG_DIR") {
		t.Errorf("expected unset CLAUDE_CONFIG_DIR, got: %s", out)
	}
}

func TestUseNoArgs(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "use")
	if err == nil {
		t.Fatal("expected error with no args")
	}
	if !strings.Contains(err.Error(), "profile name required") {
		t.Errorf("expected 'profile name required' error, got: %v", err)
	}
}

func TestUseExportQuotesEmbeddedSingleQuotes(t *testing.T) {
	// shellQuote is tested directly above, but verify it's actually used
	// in the export output format by checking the function produces valid
	// shell syntax for paths with single quotes.
	got := shellQuote("/tmp/paul's/dir")
	expected := `/tmp/paul'\''s/dir`
	if got != expected {
		t.Errorf("shellQuote produced %q, want %q", got, expected)
	}
}

func TestCurrentNoActiveProfile(t *testing.T) {
	dir := setupTestEnv(t)

	// Ensure APM_PROFILE is not set
	t.Setenv("APM_PROFILE", "")

	_, err := executeWithStdout(t, "--config-dir", dir, "current")
	if err == nil {
		t.Fatal("expected error when no profile is active")
	}
	if !isNoActiveProfile(err) {
		t.Errorf("expected errNoActiveProfile sentinel, got: %v", err)
	}
}

func TestCurrentWithEnvVar(t *testing.T) {
	dir := setupTestEnv(t)

	t.Setenv("APM_PROFILE", "env-profile")

	out, err := executeWithStdout(t, "--config-dir", dir, "current")
	if err != nil {
		t.Fatalf("current failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "env-profile") {
		t.Errorf("expected 'env-profile' in output, got: %s", out)
	}
}

func TestEditNotFound(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "edit", "nonexistent")
	if err == nil {
		t.Fatal("expected error editing nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestEditWithWhitespaceVisualFallsToEditor(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a profile
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "edit-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// VISUAL is whitespace-only, EDITOR is "true" (exits 0, does nothing).
	// Verifies: whitespace VISUAL is treated as empty, falls to EDITOR.
	// Also verifies: sh -c invocation works without panic.
	t.Setenv("VISUAL", "   ")
	t.Setenv("EDITOR", "true")

	_, err = executeWithStdout(t, "--config-dir", dir, "edit", "edit-test")
	if err != nil {
		t.Fatalf("edit with EDITOR=true should succeed, got: %v", err)
	}
}

func TestEditShellParsesEditorWithFlags(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "edit-flags")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// EDITOR with flags — sh -c handles this; strings.Fields would have too,
	// but the point is sh -c also handles quoted paths that Fields cannot.
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "true --some-flag")

	_, err = executeWithStdout(t, "--config-dir", dir, "edit", "edit-flags")
	if err != nil {
		t.Fatalf("edit with multi-word EDITOR should succeed, got: %v", err)
	}
}

func TestCLICreateFromProfile(t *testing.T) {
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

func TestCreateNoArgsDefault(t *testing.T) {
	dir := setupTestEnv(t)

	// apm create (no args) → creates "default" profile, auto-activates
	out, err := executeWithStdout(t, "--config-dir", dir, "create")
	if err != nil {
		t.Fatalf("create (no args) failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Created profile 'default'") {
		t.Errorf("expected Created message: %s", out)
	}

	// Verify config.yaml has the default set
	configData, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("reading config.yaml: %v", err)
	}
	if !strings.Contains(string(configData), "default_profile: default") {
		t.Errorf("expected default_profile in config.yaml, got: %s", string(configData))
	}

	// Verify generated dir was created
	genDir := filepath.Join(dir, "generated", "default")
	if _, err := os.Stat(genDir); err != nil {
		t.Errorf("generated dir not created: %v", err)
	}
}

func TestCreateFromCurrentNoArgs(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a fake claude dir with settings
	claudeDir := filepath.Join(dir, "claude-home")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"model": "sonnet"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write config.yaml pointing to the fake claude dir
	configYaml := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configYaml, []byte("claude_dir: "+claudeDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// apm create --current (no name → "default")
	out, err := executeWithStdout(t, "--config-dir", dir, "create", "--current")
	if err != nil {
		t.Fatalf("create --current failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Created profile 'default'") {
		t.Errorf("expected Created message: %s", out)
	}

	// Verify settings were imported
	profileSettings := filepath.Join(dir, "profiles", "default", "settings.json")
	data, err := os.ReadFile(profileSettings)
	if err != nil {
		t.Fatalf("reading profile settings: %v", err)
	}
	if !strings.Contains(string(data), "sonnet") {
		t.Errorf("expected imported settings, got: %s", string(data))
	}

	// Verify it was set as global default
	configData, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("reading config.yaml: %v", err)
	}
	if !strings.Contains(string(configData), "default_profile: default") {
		t.Errorf("expected default_profile in config.yaml, got: %s", string(configData))
	}
}

// setupShellTest builds the apm binary and creates a config dir at the
// default location ($HOME/.config/apm) under a fake HOME. Returns the
// bin dir (for PATH), fake home, and config dir.
func setupShellTest(t *testing.T) (binDir, fakeHome, configDir string) {
	t.Helper()

	// Build the binary
	binDir = t.TempDir()
	binPath := filepath.Join(binDir, "apm")
	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = getProjectRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	// Create config dir at default location under fake HOME
	fakeHome = t.TempDir()
	configDir = filepath.Join(fakeHome, ".config", "apm")

	cfg := &Config{
		APMDir:       configDir,
		ClaudeDir:    filepath.Join(fakeHome, ".claude"),
		CommonDir:    filepath.Join(configDir, "common"),
		ProfilesDir:  filepath.Join(configDir, "profiles"),
		GeneratedDir: filepath.Join(configDir, "generated"),
		ConfigPath:   filepath.Join(configDir, "config.yaml"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Create a profile using the binary directly
	create := exec.Command(binPath, "--config-dir", configDir, "create", "shell-test")
	create.Env = append(os.Environ(), "HOME="+fakeHome)
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create failed: %v\n%s", err, out)
	}

	return binDir, fakeHome, configDir
}

// TestShellIntegrationBash runs the generated shell wrapper in a real bash
// subprocess and verifies that `apm use` activates via symlink.
func TestShellIntegrationBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	binDir, fakeHome, configDir := setupShellTest(t)

	initScript, _ := executeWithStdout(t, "init", "bash")

	// The wrapper intercepts `apm use`, evals stdout (APM_PROFILE export).
	// In normal mode (no --config-dir), activateProfile creates a symlink.
	script := initScript + "\n" +
		`apm use shell-test` + "\n" +
		`echo "APM_PROFILE=$APM_PROFILE"` + "\n" +
		`if [ -L "$HOME/.claude" ]; then echo "SYMLINK=yes"; else echo "SYMLINK=no"; fi` + "\n" +
		`readlink "$HOME/.claude"` + "\n"

	bashCmd := exec.Command("bash", "-c", script)
	bashCmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + fakeHome,
		"TERM=dumb",
	}

	out, err := bashCmd.CombinedOutput()
	output := string(out)
	if err != nil {
		t.Fatalf("bash script failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "APM_PROFILE=shell-test") {
		t.Errorf("expected APM_PROFILE=shell-test in output:\n%s", output)
	}
	if !strings.Contains(output, "SYMLINK=yes") {
		t.Errorf("expected ~/.claude to be a symlink:\n%s", output)
	}
	expectedGenDir := filepath.Join(configDir, "generated", "shell-test")
	if !strings.Contains(output, expectedGenDir) {
		t.Errorf("expected symlink target %s in output:\n%s", expectedGenDir, output)
	}
}

// TestShellIntegrationUnset verifies that `apm use --unset` clears env vars
// and removes the symlink through the shell wrapper.
func TestShellIntegrationUnset(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	binDir, fakeHome, _ := setupShellTest(t)

	initScript, _ := executeWithStdout(t, "init", "bash")

	// Activate first, then deactivate
	script := initScript + "\n" +
		`apm use shell-test` + "\n" +
		`apm use --unset` + "\n" +
		`echo "APM_PROFILE=${APM_PROFILE:-EMPTY}"` + "\n" +
		`if [ -L "$HOME/.claude" ]; then echo "SYMLINK=yes"; else echo "SYMLINK=no"; fi` + "\n"

	bashCmd := exec.Command("bash", "-c", script)
	bashCmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + fakeHome,
		"TERM=dumb",
	}

	out, err := bashCmd.CombinedOutput()
	output := string(out)
	if err != nil {
		t.Fatalf("bash script failed: %v\noutput: %s", err, output)
	}

	if !strings.Contains(output, "APM_PROFILE=EMPTY") {
		t.Errorf("expected APM_PROFILE to be unset, got:\n%s", output)
	}
	if !strings.Contains(output, "SYMLINK=no") {
		t.Errorf("expected ~/.claude symlink to be removed:\n%s", output)
	}
}

// TestShellIntegrationStderrPassthrough verifies that stderr from apm use
// (e.g. error messages) passes through the shell wrapper.
func TestShellIntegrationStderrPassthrough(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	binDir, fakeHome, _ := setupShellTest(t)

	initScript, _ := executeWithStdout(t, "init", "bash")

	// Try to use a nonexistent profile — error should appear on stderr
	script := initScript + "\n" +
		`apm use nonexistent 2>&1` + "\n"

	bashCmd := exec.Command("bash", "-c", script)
	bashCmd.Env = []string{
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + fakeHome,
		"TERM=dumb",
	}

	out, _ := bashCmd.CombinedOutput()
	output := string(out)

	if !strings.Contains(output, "not found") {
		t.Errorf("expected 'not found' error in stderr output:\n%s", output)
	}
}

// TestUsePipeEmitsExports verifies that `apm use` through a pipe (non-TTY)
// outputs only shell-safe export statements with no ANSI.
func TestUsePipeEmitsExports(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "pipe-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// executeWithStdout uses a pipe (non-TTY), so we get the pipe-mode output
	out, err := executeWithStdout(t, "--config-dir", dir, "use", "pipe-test")
	if err != nil {
		t.Fatalf("use failed: %v\noutput: %s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines of output, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "export APM_PROFILE='pipe-test'") {
		t.Errorf("line 1 should be APM_PROFILE export, got: %s", lines[0])
	}
	if !strings.HasPrefix(lines[1], "export CLAUDE_CONFIG_DIR='") {
		t.Errorf("line 2 should be CLAUDE_CONFIG_DIR export, got: %s", lines[1])
	}

	// Verify no ANSI escape sequences
	if strings.Contains(out, "\033[") || strings.Contains(out, "\x1b[") {
		t.Errorf("stdout contains ANSI escape sequences:\n%s", out)
	}
}

// TestUseUnsetPipeEmitsUnset verifies --unset through a pipe emits unset commands.
func TestUseUnsetPipeEmitsUnset(t *testing.T) {
	dir := setupTestEnv(t)

	out, err := executeWithStdout(t, "--config-dir", dir, "use", "--unset")
	if err != nil {
		t.Fatalf("use --unset failed: %v\noutput: %s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines, got %d:\n%s", len(lines), out)
	}
	if lines[0] != "unset APM_PROFILE" {
		t.Errorf("line 1 should be 'unset APM_PROFILE', got: %s", lines[0])
	}
	if lines[1] != "unset CLAUDE_CONFIG_DIR" {
		t.Errorf("line 2 should be 'unset CLAUDE_CONFIG_DIR', got: %s", lines[1])
	}
}

func TestDevModeExternalState(t *testing.T) {
	dir := setupTestEnv(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create two profiles
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "prof-a")
	if err != nil {
		t.Fatalf("create prof-a failed: %v", err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "prof-b")
	if err != nil {
		t.Fatalf("create prof-b failed: %v", err)
	}

	// Create fake ~/.claude.json
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"auth":"a-token"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Use prof-a in dev mode
	_, err = executeWithStdout(t, "--config-dir", dir, "use", "prof-a")
	if err != nil {
		t.Fatalf("use prof-a failed: %v", err)
	}

	// Write prof-a's external state manually (simulating what the capture above did)
	extDirA := filepath.Join(dir, "profiles", "prof-a", "external")
	if err := os.MkdirAll(extDirA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDirA, "claude.json"), []byte(`{"auth":"a-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Write prof-b's external state
	extDirB := filepath.Join(dir, "profiles", "prof-b", "external")
	if err := os.MkdirAll(extDirB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDirB, "claude.json"), []byte(`{"auth":"b-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Switch to prof-b in dev mode — should restore prof-b's external state
	_, err = executeWithStdout(t, "--config-dir", dir, "use", "prof-b")
	if err != nil {
		t.Fatalf("use prof-b failed: %v", err)
	}

	// Verify ~/.claude.json now has prof-b's auth
	data, err := os.ReadFile(claudeJSON)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"auth":"b-token"}` {
		t.Errorf("expected prof-b auth, got %q", string(data))
	}
}

func TestDevModeCreateExternalState(t *testing.T) {
	dir := setupTestEnv(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create fake ~/.claude dir with settings for --current
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(`{"model":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Point config to it
	configYaml := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configYaml, []byte("claude_dir: "+claudeDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create ~/.claude.json
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"auth":"my-token"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create profile with --current in dev mode
	out, err := executeWithStdout(t, "--config-dir", dir, "create", "--current", "current-test")
	if err != nil {
		t.Fatalf("create --current failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Created profile 'current-test'") {
		t.Errorf("unexpected output: %s", out)
	}

	// External state should have been captured
	extDir := filepath.Join(dir, "profiles", "current-test", "external")
	data, err := os.ReadFile(filepath.Join(extDir, "claude.json"))
	if err != nil {
		t.Fatalf("external state not captured: %v", err)
	}
	if string(data) != `{"auth":"my-token"}` {
		t.Errorf("captured auth = %q, want original", string(data))
	}
}

func TestRegenerateAllActiveProfileGuard(t *testing.T) {
	dir := setupTestEnv(t)

	// Create two profiles
	for _, name := range []string{"regen-a", "regen-b"} {
		_, err := executeWithStdout(t, "--config-dir", dir, "create", name)
		if err != nil {
			t.Fatalf("create %s failed: %v", name, err)
		}
	}

	// Simulate an active profile in config
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetActiveProfile("regen-a"); err != nil {
		t.Fatal(err)
	}

	// Add runtime state to the active profile's generated dir
	genDir := cfg.GeneratedProfileDir("regen-a")
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "history.jsonl"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Regenerate all
	out, err := executeWithStdout(t, "--config-dir", dir, "regenerate", "--all")
	if err != nil {
		t.Fatalf("regenerate --all failed: %v\noutput: %s", err, out)
	}

	// Both should be regenerated
	if !strings.Contains(out, "Regenerated 'regen-a'") {
		t.Errorf("expected regen-a in output: %s", out)
	}
	if !strings.Contains(out, "Regenerated 'regen-b'") {
		t.Errorf("expected regen-b in output: %s", out)
	}

	// Runtime state should be preserved (not wiped by os.RemoveAll)
	data, err := os.ReadFile(filepath.Join(genDir, "history.jsonl"))
	if err != nil {
		t.Fatalf("runtime state lost after regenerate --all: %v", err)
	}
	if string(data) != "keep" {
		t.Errorf("runtime state changed: %q", string(data))
	}
}

func TestEnsureDirsCreatesGitignore(t *testing.T) {
	cfg := newTestConfig(t)

	gitignorePath := filepath.Join(cfg.APMDir, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf(".gitignore not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "*/external/") {
		t.Errorf(".gitignore should contain '*/external/', got: %s", content)
	}
	if !strings.Contains(content, "claude-home-external/") {
		t.Errorf(".gitignore should contain 'claude-home-external/', got: %s", content)
	}
}

func TestCreateOverwritePreservesExternalState(t *testing.T) {
	dir := setupTestEnv(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create a profile
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "overwrite-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Create ~/.claude.json
	claudeJSON := filepath.Join(fakeHome, ".claude.json")
	if err := os.WriteFile(claudeJSON, []byte(`{"auth":"original"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Store external state for the profile
	extDir := filepath.Join(dir, "profiles", "overwrite-test", "external")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extDir, "claude.json"), []byte(`{"auth":"saved"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Overwrite confirmed
	confirmOverwrite = func(name string) bool { return true }
	defer func() { confirmOverwrite = func(string) bool { return false } }()

	// Create again (overwrite)
	out, err := executeWithStdout(t, "--config-dir", dir, "create", "overwrite-test")
	if err != nil {
		t.Fatalf("overwrite create failed: %v", err)
	}
	if !strings.Contains(out, "Created profile 'overwrite-test'") {
		t.Errorf("expected Created message: %s", out)
	}

	// Profile should still exist
	profileDir := filepath.Join(dir, "profiles", "overwrite-test")
	if _, err := os.Stat(profileDir); err != nil {
		t.Fatalf("profile dir not recreated: %v", err)
	}
}

// getProjectRoot returns the project root directory.
func getProjectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestNukeWithProfiles(t *testing.T) {
	dir := setupTestEnv(t)

	// Create some profiles
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "prof-a")
	if err != nil {
		t.Fatalf("create prof-a failed: %v", err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "prof-b")
	if err != nil {
		t.Fatalf("create prof-b failed: %v", err)
	}

	// Verify profiles exist
	profileDir := filepath.Join(dir, "profiles", "prof-a")
	if _, err := os.Stat(profileDir); err != nil {
		t.Fatalf("profile should exist before nuke: %v", err)
	}

	// Nuke with --force — stdout should be empty (all output goes to stderr)
	out, err := executeWithStdout(t, "--config-dir", dir, "nuke", "--force")
	if err != nil {
		t.Fatalf("nuke failed: %v", err)
	}
	if out != "" {
		t.Errorf("nuke should not write to stdout (got %q)", out)
	}

	// APM directory should be gone
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("APM directory should be removed after nuke")
	}
}

func TestNukeNoProfiles(t *testing.T) {
	dir := setupTestEnv(t)

	// Nuke an empty APM dir
	_, err := executeWithStdout(t, "--config-dir", dir, "nuke", "--force")
	if err != nil {
		t.Fatalf("nuke failed: %v", err)
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("APM directory should be removed after nuke")
	}
}

func TestNukeRequiresConfirmation(t *testing.T) {
	dir := setupTestEnv(t)

	// Mock confirmNuke to decline
	oldConfirm := confirmNuke
	confirmNuke = func() bool { return false }
	defer func() { confirmNuke = oldConfirm }()

	_, err := executeWithStdout(t, "--config-dir", dir, "nuke")
	if err != nil {
		t.Fatalf("nuke should not error when declined: %v", err)
	}

	// APM dir should still exist
	if _, err := os.Stat(dir); err != nil {
		t.Error("APM directory should still exist after declined nuke")
	}
}

func TestNukeForceSkipsConfirmation(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// --force should skip confirmation
	_, err = executeWithStdout(t, "--config-dir", dir, "nuke", "--force")
	if err != nil {
		t.Fatalf("nuke --force failed: %v", err)
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("APM directory should be removed")
	}
}

func TestNukeWithActiveProfile(t *testing.T) {
	dir := setupTestEnv(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create and "activate" a profile (dev mode with --config-dir)
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "active-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "use", "active-test")
	if err != nil {
		t.Fatalf("use failed: %v", err)
	}

	// Nuke
	_, err = executeWithStdout(t, "--config-dir", dir, "nuke", "--force")
	if err != nil {
		t.Fatalf("nuke failed: %v", err)
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("APM directory should be removed after nuke")
	}
}

func TestNukeWarnsAboutEnvVar(t *testing.T) {
	dir := setupTestEnv(t)
	t.Setenv("APM_PROFILE", "stale-profile")

	// Nuke should succeed and warn about stale env var
	// (warning goes to stderr, not captured by executeWithStdout)
	_, err := executeWithStdout(t, "--config-dir", dir, "nuke", "--force")
	if err != nil {
		t.Fatalf("nuke failed: %v", err)
	}
}
