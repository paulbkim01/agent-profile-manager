package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

	// Set up the directory structure
	for _, d := range []string{
		filepath.Join(configDir, "common", "skills"),
		filepath.Join(configDir, "common", "commands"),
		filepath.Join(configDir, "common", "agents"),
		filepath.Join(configDir, "profiles"),
		filepath.Join(configDir, "generated"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(configDir, "common", "settings.json"),
		[]byte("{}\n"), 0o644,
	); err != nil {
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
// subprocess and verifies that `apm use` sets env vars via eval.
func TestShellIntegrationBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	binDir, fakeHome, configDir := setupShellTest(t)

	initScript, _ := executeWithStdout(t, "init", "bash")

	// The wrapper checks $1 for "use", so call `apm use shell-test` directly.
	// The wrapper intercepts it, runs `command apm use shell-test`, evals the output.
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
	expectedGenDir := filepath.Join(configDir, "generated", "shell-test")
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

	// Start with env vars set, then unset them via wrapper
	script := initScript + "\n" +
		`export APM_PROFILE=old-profile` + "\n" +
		`export CLAUDE_CONFIG_DIR=/old/path` + "\n" +
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

// getProjectRoot returns the project root directory.
func getProjectRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(wd)
}
