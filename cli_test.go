package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
	useGlobal = false
	debug = false
	configDir = ""
	nukeForce = false
	editEnv = false
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

// executeCaptureBoth runs the root command and captures both stdout and stderr.
// Returns (stdout, stderr, error).
func executeCaptureBoth(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetFlags()

	// Capture stdout
	oldStdout := os.Stdout
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = wOut

	// Capture stderr
	oldStderr := os.Stderr
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = wErr

	rootCmd.SetArgs(args)
	execErr := rootCmd.Execute()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var outBuf, errBuf bytes.Buffer
	outBuf.ReadFrom(rOut)
	errBuf.ReadFrom(rErr)
	return outBuf.String(), errBuf.String(), execErr
}

func TestCreateAndList(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a profile
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "create", "test-profile", "--description", "a test profile")
	if err != nil {
		t.Fatalf("create failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Created profile 'test-profile'") {
		t.Errorf("unexpected create output: %s", stderr)
	}

	// Verify profile dir exists
	profileDir := filepath.Join(dir, "profiles", "test-profile")
	if _, err := os.Stat(profileDir); err != nil {
		t.Fatalf("profile dir not created: %v", err)
	}

	// List profiles
	out, err := executeWithStdout(t, "--config-dir", dir, "ls")
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

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "create", "dup")
	if err != nil {
		t.Fatalf("overwrite create failed: %v", err)
	}
	if !strings.Contains(stderr, "Created profile 'dup'") {
		t.Errorf("expected Created message: %s", stderr)
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
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "regenerate", "regen-test")
	if err != nil {
		t.Fatalf("regenerate failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Regenerated 'regen-test'") {
		t.Errorf("unexpected regenerate output: %s", stderr)
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

	// Regenerate all (no args = all)
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "regenerate")
	if err != nil {
		t.Fatalf("regenerate failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Regenerated 'prof-a'") {
		t.Errorf("expected prof-a in output: %s", stderr)
	}
	if !strings.Contains(stderr, "Regenerated 'prof-b'") {
		t.Errorf("expected prof-b in output: %s", stderr)
	}
}

func TestRegenerateNoArgs(t *testing.T) {
	dir := setupTestEnv(t)

	// No args = regenerate all (no profiles exist, so "No profiles" message)
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "regenerate")
	if err != nil {
		t.Fatalf("regenerate failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "No profiles to regenerate") {
		t.Errorf("expected 'No profiles to regenerate' in stderr, got: %s", stderr)
	}
}

func TestRegenerateAlias(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "alias-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Use the "regen" alias
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "regen", "alias-test")
	if err != nil {
		t.Fatalf("regen alias failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Regenerated 'alias-test'") {
		t.Errorf("unexpected regen output: %s", stderr)
	}
}

func TestCLIListEmpty(t *testing.T) {
	dir := setupTestEnv(t)

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "ls")
	if err != nil {
		t.Fatalf("ls failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "No profiles") {
		t.Errorf("expected 'No profiles' message: %s", stderr)
	}
	if !strings.Contains(stderr, "apm create") {
		t.Errorf("expected nudge with 'apm create': %s", stderr)
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
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "create", "derived-prof", "--from", "source-prof")
	if err != nil {
		t.Fatalf("create --from failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Created profile 'derived-prof'") {
		t.Errorf("unexpected output: %s", stderr)
	}
	// Should not show "Edit it with" when --from is used
	if strings.Contains(stderr, "Edit it with") {
		t.Errorf("should not show edit hint when --from is used: %s", stderr)
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
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "create")
	if err != nil {
		t.Fatalf("create (no args) failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Created profile 'default'") {
		t.Errorf("expected Created message: %s", stderr)
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
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "create", "--current")
	if err != nil {
		t.Fatalf("create --current failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Created profile 'default'") {
		t.Errorf("expected Created message: %s", stderr)
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
func setupShellTest(t *testing.T) (binDir, fakeHome, cfgDir string) {
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
	cfgDir = filepath.Join(fakeHome, ".config", "apm")

	cfg := &Config{
		APMDir:       cfgDir,
		ClaudeDir:    filepath.Join(fakeHome, ".claude"),
		CommonDir:    filepath.Join(cfgDir, "common"),
		ProfilesDir:  filepath.Join(cfgDir, "profiles"),
		GeneratedDir: filepath.Join(cfgDir, "generated"),
		ConfigPath:   filepath.Join(cfgDir, "config.yaml"),
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Create a profile using the binary directly
	create := exec.Command(binPath, "--config-dir", cfgDir, "create", "shell-test")
	create.Env = append(os.Environ(), "HOME="+fakeHome)
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create failed: %v\n%s", err, out)
	}

	return binDir, fakeHome, cfgDir
}

// TestShellIntegrationBash runs the generated shell wrapper in a real bash
// subprocess and verifies that `apm use` exports CLAUDE_CONFIG_DIR.
func TestShellIntegrationBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	binDir, fakeHome, cfgDir := setupShellTest(t)

	initScript, _ := executeWithStdout(t, "init", "bash")

	// The wrapper intercepts `apm use`, evals stdout (exports).
	script := initScript + "\n" +
		`apm use shell-test` + "\n" +
		`echo "APM_PROFILE=$APM_PROFILE"` + "\n" +
		`echo "CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR"` + "\n"

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
	expectedGenDir := filepath.Join(cfgDir, "generated", "shell-test")
	if !strings.Contains(output, "CLAUDE_CONFIG_DIR="+expectedGenDir) {
		t.Errorf("expected CLAUDE_CONFIG_DIR=%s in output:\n%s", expectedGenDir, output)
	}
}

// TestShellIntegrationUnset verifies that `apm use --unset` clears env vars
// through the shell wrapper.
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
		`echo "CLAUDE_CONFIG_DIR=${CLAUDE_CONFIG_DIR:-EMPTY}"` + "\n"

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
	if !strings.Contains(output, "CLAUDE_CONFIG_DIR=EMPTY") {
		t.Errorf("expected CLAUDE_CONFIG_DIR to be unset, got:\n%s", output)
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

func TestRegenerateAllActiveProfileGuard(t *testing.T) {
	dir := setupTestEnv(t)

	// Create two profiles
	for _, name := range []string{"regen-a", "regen-b"} {
		_, err := executeWithStdout(t, "--config-dir", dir, "create", name)
		if err != nil {
			t.Fatalf("create %s failed: %v", name, err)
		}
	}

	// Simulate an active profile via env var
	t.Setenv("APM_PROFILE", "regen-a")

	cfg, err := loadConfig(dir)
	if err != nil {
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

	// Regenerate all (no args = all)
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "regenerate")
	if err != nil {
		t.Fatalf("regenerate failed: %v\nstderr: %s", err, stderr)
	}

	// Both should be regenerated
	if !strings.Contains(stderr, "Regenerated 'regen-a'") {
		t.Errorf("expected regen-a in output: %s", stderr)
	}
	if !strings.Contains(stderr, "Regenerated 'regen-b'") {
		t.Errorf("expected regen-b in output: %s", stderr)
	}

	// Runtime state should be preserved (not wiped by os.RemoveAll)
	data, err := os.ReadFile(filepath.Join(genDir, "history.jsonl"))
	if err != nil {
		t.Fatalf("runtime state lost after regenerate: %v", err)
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
	if !strings.Contains(content, "generated/*/.claude.json") {
		t.Errorf(".gitignore should contain 'generated/*/.claude.json', got: %s", content)
	}
}

func TestCreateOverwriteWorks(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a profile
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "overwrite-test")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Overwrite confirmed
	confirmOverwrite = func(name string) bool { return true }
	defer func() { confirmOverwrite = func(string) bool { return false } }()

	// Create again (overwrite)
	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "create", "overwrite-test")
	if err != nil {
		t.Fatalf("overwrite create failed: %v", err)
	}
	if !strings.Contains(stderr, "Created profile 'overwrite-test'") {
		t.Errorf("expected Created message: %s", stderr)
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

	// Profiles and generated dirs should be gone
	if _, err := os.Stat(filepath.Join(dir, "profiles")); !errors.Is(err, os.ErrNotExist) {
		t.Error("profiles directory should be removed after nuke")
	}
	if _, err := os.Stat(filepath.Join(dir, "generated")); !errors.Is(err, os.ErrNotExist) {
		t.Error("generated directory should be removed after nuke")
	}

	// Common directory should still exist
	if _, err := os.Stat(filepath.Join(dir, "common")); err != nil {
		t.Error("common directory should be preserved after nuke")
	}

	// APM directory should still exist
	if _, err := os.Stat(dir); err != nil {
		t.Error("APM directory should still exist after nuke")
	}
}

func TestNukeNoProfiles(t *testing.T) {
	dir := setupTestEnv(t)

	// Nuke an empty APM dir
	_, err := executeWithStdout(t, "--config-dir", dir, "nuke", "--force")
	if err != nil {
		t.Fatalf("nuke failed: %v", err)
	}

	// Profiles and generated dirs should be gone
	if _, err := os.Stat(filepath.Join(dir, "profiles")); !errors.Is(err, os.ErrNotExist) {
		t.Error("profiles directory should be removed after nuke")
	}
	if _, err := os.Stat(filepath.Join(dir, "generated")); !errors.Is(err, os.ErrNotExist) {
		t.Error("generated directory should be removed after nuke")
	}

	// Common should be preserved
	if _, err := os.Stat(filepath.Join(dir, "common")); err != nil {
		t.Error("common directory should be preserved after nuke")
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

	// Profiles should be gone
	if _, err := os.Stat(filepath.Join(dir, "profiles")); !errors.Is(err, os.ErrNotExist) {
		t.Error("profiles directory should be removed")
	}

	// Common should be preserved
	if _, err := os.Stat(filepath.Join(dir, "common")); err != nil {
		t.Error("common directory should be preserved")
	}
}

func TestNukeWithActiveProfile(t *testing.T) {
	dir := setupTestEnv(t)
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	// Create and activate a profile
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

	// Profiles should be gone
	if _, err := os.Stat(filepath.Join(dir, "profiles")); !errors.Is(err, os.ErrNotExist) {
		t.Error("profiles directory should be removed after nuke")
	}

	// Common should be preserved
	if _, err := os.Stat(filepath.Join(dir, "common")); err != nil {
		t.Error("common directory should be preserved after nuke")
	}
}

func TestNukeWarnsAboutEnvVar(t *testing.T) {
	dir := setupTestEnv(t)
	t.Setenv("APM_PROFILE", "stale-profile")

	// Nuke should succeed
	_, err := executeWithStdout(t, "--config-dir", dir, "nuke", "--force")
	if err != nil {
		t.Fatalf("nuke failed: %v", err)
	}
}

func TestCompleteProfileNames(t *testing.T) {
	dir := setupTestEnv(t)
	configDir = dir

	// Create some profiles
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "alpha", "--description", "Alpha profile")
	if err != nil {
		t.Fatalf("create alpha failed: %v", err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "beta")
	if err != nil {
		t.Fatalf("create beta failed: %v", err)
	}

	// Test completion returns profile names
	names, directive := completeProfileNames(useCmd, []string{}, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("expected NoFileComp directive, got %v", directive)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 completions, got %d: %v", len(names), names)
	}

	// Check that profile with description includes it as tab-separated annotation
	foundAlphaWithDesc := false
	foundBeta := false
	for _, n := range names {
		if strings.HasPrefix(n, "alpha\t") {
			foundAlphaWithDesc = true
		}
		if n == "beta" {
			foundBeta = true
		}
	}
	if !foundAlphaWithDesc {
		t.Errorf("expected alpha with tab-separated description, got: %v", names)
	}
	if !foundBeta {
		t.Errorf("expected beta without description, got: %v", names)
	}

	// Test that providing an arg already returns no completions
	names2, _ := completeProfileNames(useCmd, []string{"alpha"}, "")
	if len(names2) != 0 {
		t.Errorf("expected 0 completions when arg already provided, got %d", len(names2))
	}
}

func TestRenameBasic(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "old-name", "--description", "rename me")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "rename", "old-name", "new-name")
	if err != nil {
		t.Fatalf("rename failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Renamed 'old-name' → 'new-name'") {
		t.Errorf("unexpected rename output: %s", stderr)
	}

	// Old profile should not exist
	if _, err := os.Stat(filepath.Join(dir, "profiles", "old-name")); !errors.Is(err, os.ErrNotExist) {
		t.Error("old profile dir should not exist after rename")
	}

	// New profile should exist with correct metadata
	out, err := executeWithStdout(t, "--config-dir", dir, "describe", "new-name")
	if err != nil {
		t.Fatalf("describe new-name failed: %v", err)
	}
	if !strings.Contains(out, "Profile: new-name") {
		t.Errorf("expected new-name in describe output: %s", out)
	}
	if !strings.Contains(out, "Description: rename me") {
		t.Errorf("expected description preserved: %s", out)
	}
}

func TestRenameActiveProfile(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "active-rename")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	// Set as default (use --global)
	_, err = executeWithStdout(t, "--config-dir", dir, "use", "active-rename", "--global")
	if err != nil {
		t.Fatalf("use --global failed: %v", err)
	}

	_, _, err = executeCaptureBoth(t, "--config-dir", dir, "rename", "active-rename", "renamed-active")
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	// Config default_profile should reference new name
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	def, _ := cfg.DefaultProfile()
	if def != "renamed-active" {
		t.Errorf("default profile = %q, want 'renamed-active'", def)
	}
}

func TestRenameDefaultProfile(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "default")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	_, _, err = executeCaptureBoth(t, "--config-dir", dir, "rename", "default", "primary")
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	def, _ := cfg.DefaultProfile()
	if def != "primary" {
		t.Errorf("default profile = %q, want 'primary'", def)
	}
}

func TestRenameNotFound(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "rename", "nonexistent", "new-name")
	if err == nil {
		t.Fatal("expected error renaming nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestRenameToExisting(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "first")
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "second")
	if err != nil {
		t.Fatal(err)
	}

	_, err = executeWithStdout(t, "--config-dir", dir, "rename", "first", "second")
	if err == nil {
		t.Fatal("expected error renaming to existing profile")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestRenameInvalidName(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "valid")
	if err != nil {
		t.Fatal(err)
	}

	_, err = executeWithStdout(t, "--config-dir", dir, "rename", "valid", "INVALID")
	if err == nil {
		t.Fatal("expected error for invalid new name")
	}
}

func TestRenameAlias(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "mv-test")
	if err != nil {
		t.Fatal(err)
	}

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "mv", "mv-test", "moved")
	if err != nil {
		t.Fatalf("mv alias failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Renamed") {
		t.Errorf("expected Renamed message: %s", stderr)
	}
}

func TestRenameRegeneratesSymlinks(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "sym-before")
	if err != nil {
		t.Fatal(err)
	}

	// Add a skill to the profile
	skillsDir := filepath.Join(dir, "profiles", "sym-before", "skills")
	os.MkdirAll(skillsDir, 0o755)
	os.WriteFile(filepath.Join(skillsDir, "test.md"), []byte("skill content"), 0o644)

	// Regenerate to create symlinks
	_, err = executeWithStdout(t, "--config-dir", dir, "regenerate", "sym-before")
	if err != nil {
		t.Fatal(err)
	}

	// Rename
	_, _, err = executeCaptureBoth(t, "--config-dir", dir, "rename", "sym-before", "sym-after")
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	// Verify the symlink in generated dir points to the new profile path
	genSkill := filepath.Join(dir, "generated", "sym-after", "skills", "test.md")
	target, err := os.Readlink(genSkill)
	if err != nil {
		t.Fatalf("symlink should exist: %v", err)
	}

	// Target should contain "sym-after", not "sym-before"
	if strings.Contains(target, "sym-before") {
		t.Errorf("symlink still points to old path: %s", target)
	}
	if !strings.Contains(target, "sym-after") {
		t.Errorf("symlink should point to new path, got: %s", target)
	}

	// Verify the content is accessible through the symlink
	data, err := os.ReadFile(genSkill)
	if err != nil {
		t.Fatalf("reading through symlink failed: %v", err)
	}
	if string(data) != "skill content" {
		t.Errorf("content wrong: %q", string(data))
	}
}

func TestRenamePreservesRuntimeState(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "rt-before")
	if err != nil {
		t.Fatal(err)
	}

	// Plant runtime state files in the generated directory
	genDir := filepath.Join(dir, "generated", "rt-before")
	os.MkdirAll(genDir, 0o755)
	os.WriteFile(filepath.Join(genDir, ".claude.json"), []byte(`{"auth":"token123"}`), 0o600)
	os.WriteFile(filepath.Join(genDir, "history.jsonl"), []byte(`{"event":"test"}`), 0o644)
	os.MkdirAll(filepath.Join(genDir, "sessions", "abc"), 0o755)
	os.WriteFile(filepath.Join(genDir, "sessions", "abc", "data.json"), []byte(`{}`), 0o644)

	// Rename
	_, _, err = executeCaptureBoth(t, "--config-dir", dir, "rename", "rt-before", "rt-after")
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	// Verify runtime state survived in the new generated dir
	newGen := filepath.Join(dir, "generated", "rt-after")

	data, err := os.ReadFile(filepath.Join(newGen, ".claude.json"))
	if err != nil {
		t.Fatalf(".claude.json lost after rename: %v", err)
	}
	if string(data) != `{"auth":"token123"}` {
		t.Errorf(".claude.json content wrong: %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(newGen, "history.jsonl"))
	if err != nil {
		t.Fatalf("history.jsonl lost after rename: %v", err)
	}
	if string(data) != `{"event":"test"}` {
		t.Errorf("history.jsonl content wrong: %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(newGen, "sessions", "abc", "data.json"))
	if err != nil {
		t.Fatalf("sessions dir lost after rename: %v", err)
	}
}

func TestDiffIdentical(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "diff-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "diff-b")
	if err != nil {
		t.Fatal(err)
	}

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "diff", "diff-a", "diff-b")
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}
	if !strings.Contains(stderr, "No differences found") {
		t.Errorf("expected 'No differences found', got: %s", stderr)
	}
}

func TestDiffSettings(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "diff-set-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "diff-set-b")
	if err != nil {
		t.Fatal(err)
	}

	// Write different settings
	os.WriteFile(filepath.Join(dir, "profiles", "diff-set-a", "settings.json"), []byte(`{"model":"opus"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "profiles", "diff-set-b", "settings.json"), []byte(`{"model":"sonnet"}`), 0o644)

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "diff", "diff-set-a", "diff-set-b")
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}
	if strings.Contains(stderr, "No differences found") {
		t.Error("should have found differences")
	}
	if !strings.Contains(stderr, "model") {
		t.Errorf("expected 'model' in diff output: %s", stderr)
	}
}

func TestDiffEnvVars(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "diff-env-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "diff-env-b")
	if err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(dir, "profiles", "diff-env-a", "env"), []byte("AWS_REGION=us-east-1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "profiles", "diff-env-b", "env"), []byte("AWS_REGION=eu-west-1\n"), 0o644)

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "diff", "diff-env-a", "diff-env-b")
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}
	if !strings.Contains(stderr, "AWS_REGION") {
		t.Errorf("expected AWS_REGION in diff output: %s", stderr)
	}
}

func TestDiffNotFound(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "exists")
	if err != nil {
		t.Fatal(err)
	}

	_, err = executeWithStdout(t, "--config-dir", dir, "diff", "exists", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestDiffInvalidName(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "diff", "INVALID", "also-bad")
	if err == nil {
		t.Fatal("expected error for invalid profile name")
	}
}

func TestDoctorClean(t *testing.T) {
	dir := setupTestEnv(t)

	// Create and activate a profile for a clean state
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "doc-test")
	if err != nil {
		t.Fatal(err)
	}

	// Set APM_PROFILE so doctor doesn't warn about shell integration
	t.Setenv("APM_PROFILE", "doc-test")

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "No issues found") {
		t.Errorf("expected 'No issues found' for clean state: %s", stderr)
	}
}

func TestDoctorOrphanedGenerated(t *testing.T) {
	dir := setupTestEnv(t)

	// Create an orphaned generated directory (no matching profile)
	orphanDir := filepath.Join(dir, "generated", "ghost")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Orphaned generated dir") {
		t.Errorf("expected orphaned dir warning: %s", stderr)
	}
	if !strings.Contains(stderr, "ghost") {
		t.Errorf("expected 'ghost' in warning: %s", stderr)
	}
}

func TestDoctorStaleDefaultProfile(t *testing.T) {
	dir := setupTestEnv(t)

	// Set default_profile to a nonexistent profile
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetDefaultProfile("deleted-profile"); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "not found") {
		t.Errorf("expected 'not found' warning for stale default profile: %s", stderr)
	}
}

func TestDoctorInvalidSettings(t *testing.T) {
	dir := setupTestEnv(t)

	_, err := executeWithStdout(t, "--config-dir", dir, "create", "bad-settings")
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt the settings.json
	settingsPath := filepath.Join(dir, "profiles", "bad-settings", "settings.json")
	os.WriteFile(settingsPath, []byte(`{broken json`), 0o644)

	_, stderr, err := executeCaptureBoth(t, "--config-dir", dir, "doctor")
	if err != nil {
		t.Fatalf("doctor failed: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Invalid settings.json") {
		t.Errorf("expected invalid settings warning: %s", stderr)
	}
	if !strings.Contains(stderr, "bad-settings") {
		t.Errorf("expected profile name in warning: %s", stderr)
	}
}

// Fix 4: Verify use --unset reads APM_PROFILE env var, not config
func TestUseUnsetReadsEnvVar(t *testing.T) {
	dir := setupTestEnv(t)

	// Create a profile with env vars
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "env-test")
	if err != nil {
		t.Fatal(err)
	}

	// Write env file to profile
	envPath := filepath.Join(dir, "profiles", "env-test", "env")
	os.WriteFile(envPath, []byte("MY_VAR=hello\n"), 0o644)

	// Regenerate to produce merged env file in generated dir
	_, err = executeWithStdout(t, "--config-dir", dir, "regenerate", "env-test")
	if err != nil {
		t.Fatal(err)
	}

	// Set APM_PROFILE env to simulate an active shell
	t.Setenv("APM_PROFILE", "env-test")

	// use --unset should read the env file from the env-test profile
	out, err := executeWithStdout(t, "--config-dir", dir, "use", "--unset")
	if err != nil {
		t.Fatalf("use --unset failed: %v", err)
	}
	if !strings.Contains(out, "unset MY_VAR") {
		t.Errorf("expected 'unset MY_VAR' in output, got: %s", out)
	}
}

// Fix 5: Verify apm current returns default_profile, not active_profile
func TestCurrentSkipsActiveProfile(t *testing.T) {
	dir := setupTestEnv(t)
	t.Setenv("APM_PROFILE", "")

	// Create profiles
	_, err := executeWithStdout(t, "--config-dir", dir, "create", "foo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeWithStdout(t, "--config-dir", dir, "create", "bar")
	if err != nil {
		t.Fatal(err)
	}

	// Manually write active_profile + default_profile to config.yaml
	// (simulating a legacy config that still has active_profile)
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte("active_profile: foo\ndefault_profile: bar\n"), 0o644)

	// apm current should return "bar" (default_profile), NOT "foo" (active_profile)
	out, err := executeWithStdout(t, "--config-dir", dir, "current")
	if err != nil {
		t.Fatalf("current failed: %v", err)
	}
	out = strings.TrimSpace(out)
	if out != "bar" {
		t.Errorf("current = %q, want %q (should use default_profile, not active_profile)", out, "bar")
	}
}

// ---------------------------------------------------------------------------
// Shell init script tests
// ---------------------------------------------------------------------------

func TestInitScriptBash(t *testing.T) {
	script, err := shellInitScript("bash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain the shell function wrapper
	if !strings.Contains(script, "apm()") {
		t.Error("bash script missing apm() function")
	}
	// Should contain auto-activate
	if !strings.Contains(script, "_apm_auto_activate") {
		t.Error("bash script missing _apm_auto_activate")
	}
	// Should use POSIX-style test brackets
	if !strings.Contains(script, `[ -z "$APM_PROFILE" ]`) {
		t.Error("bash script should use [ -z for test")
	}
	// Should intercept "use" and "nuke" subcommands
	if !strings.Contains(script, "use|nuke)") {
		t.Error("bash script missing use|nuke case")
	}
	// Should use 'command apm' to avoid recursion
	if !strings.Contains(script, "command apm") {
		t.Error("bash script should use 'command apm' to bypass function")
	}
	// Should include tab completion sourcing
	if !strings.Contains(script, "apm completion bash") {
		t.Error("bash script missing completion sourcing")
	}
}

func TestInitScriptZsh(t *testing.T) {
	script, err := shellInitScript("zsh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should contain the shell function wrapper
	if !strings.Contains(script, "apm()") {
		t.Error("zsh script missing apm() function")
	}
	// Should contain auto-activate
	if !strings.Contains(script, "_apm_auto_activate") {
		t.Error("zsh script missing _apm_auto_activate")
	}
	// Should use zsh-style double brackets
	if !strings.Contains(script, `[[ -z "$APM_PROFILE" ]]`) {
		t.Error("zsh script should use [[ -z for test")
	}
	// Should include tab completion sourcing
	if !strings.Contains(script, "apm completion zsh") {
		t.Error("zsh script missing completion sourcing")
	}
}

func TestInitScriptUnsupported(t *testing.T) {
	_, err := shellInitScript("fish")
	if err == nil {
		t.Fatal("expected error for unsupported shell")
	}
	if !strings.Contains(err.Error(), "unsupported shell") {
		t.Errorf("error should mention 'unsupported shell', got: %v", err)
	}
	if !strings.Contains(err.Error(), "fish") {
		t.Errorf("error should mention the shell name, got: %v", err)
	}
}

func TestInitScriptEmpty(t *testing.T) {
	_, err := shellInitScript("")
	if err == nil {
		t.Fatal("expected error for empty shell type")
	}
}
