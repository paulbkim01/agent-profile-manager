package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
)

//go:embed statusline.sh
var defaultStatusLineScript []byte

// statusLineCommand returns the command string for the statusLine config.
// Uses $HOME for a stable path that works whether or not apm is active.
func statusLineCommand(commonDir string) string {
	return "bash " + filepath.Join(commonDir, "statusline.sh")
}

// defaultStatusLineConfig returns the statusLine object to inject into settings.
func defaultStatusLineConfig(commonDir string) map[string]any {
	return map[string]any{
		"type":    "command",
		"command": statusLineCommand(commonDir),
	}
}

// defaultStatusLineSettings returns common/settings.json content with the
// statusLine config pre-wired to the embedded script.
func defaultStatusLineSettings(commonDir string) []byte {
	settings := map[string]any{
		"statusLine": defaultStatusLineConfig(commonDir),
	}
	out, _ := json.MarshalIndent(settings, "", "  ")
	out = append(out, '\n')
	return out
}

// writeDefaultStatusLine writes the default statusline.sh to common/ if absent,
// and patches both common/settings.json and claudeDir/settings.json to include
// the statusLine config if missing. Patching claudeDir ensures the statusline
// works even when apm is not activated (CLAUDE_CONFIG_DIR not set).
func writeDefaultStatusLine(commonDir, claudeDir string) error {
	dst := filepath.Join(commonDir, "statusline.sh")

	if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(dst, defaultStatusLineScript, 0o755); err != nil {
			return err
		}
		log.Printf("config: created default %s", dst)
	} else if err != nil {
		return err
	}

	// Patch common/settings.json
	if err := ensureStatusLineInSettings(filepath.Join(commonDir, settingsFile), commonDir); err != nil {
		return err
	}

	// Also patch ~/.claude/settings.json so the statusline works without
	// apm shell activation (CLAUDE_CONFIG_DIR unset).
	claudeSettings := filepath.Join(claudeDir, settingsFile)
	if _, err := os.Stat(claudeSettings); err == nil {
		if err := ensureStatusLineInSettings(claudeSettings, commonDir); err != nil {
			log.Printf("config: warning: could not patch %s: %v", claudeSettings, err)
		}
	}

	return nil
}

// ensureStatusLineInSettings adds the statusLine key to settings.json if absent.
func ensureStatusLineInSettings(path, commonDir string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil // malformed JSON — don't touch it
	}

	if _, ok := settings["statusLine"]; ok {
		return nil // already configured
	}

	// Inject default statusLine config
	settings["statusLine"] = defaultStatusLineConfig(commonDir)
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := atomicWriteFile(path, out, 0o644); err != nil {
		return err
	}
	log.Printf("config: added statusLine to %s", path)
	return nil
}
