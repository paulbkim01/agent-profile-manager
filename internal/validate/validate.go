package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/paulbkim/agent-profile-manager/internal"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

const maxNameLen = 50

// ProfileName checks if a profile name is valid.
// Returns nil if valid, descriptive error if not.
func ProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("profile name too long (max %d chars)", maxNameLen)
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("profile name must be lowercase alphanumeric with hyphens (e.g. 'my-work')")
	}
	if internal.ReservedNames[name] {
		return fmt.Errorf("'%s' is a reserved name", name)
	}
	return nil
}

// SettingsJSON validates that the file at path is valid JSON
// and parses as an object (not array, string, etc.).
func SettingsJSON(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // missing file is valid (treated as empty {})
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	if obj == nil {
		return fmt.Errorf("invalid JSON in %s: must be an object, not null", path)
	}
	return nil
}
