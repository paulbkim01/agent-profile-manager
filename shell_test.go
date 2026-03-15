package main

import (
	"strings"
	"testing"
)

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
	// Should intercept "use" subcommand
	if !strings.Contains(script, "use)") {
		t.Error("bash script missing use case")
	}
	// Should use 'command apm' to avoid recursion
	if !strings.Contains(script, "command apm") {
		t.Error("bash script should use 'command apm' to bypass function")
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
