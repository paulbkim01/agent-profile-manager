package main

import (
	"fmt"
	"regexp"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

const maxNameLen = 50

// validateProfileName checks if a profile name is valid.
// Returns nil if valid, descriptive error if not.
func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("profile name too long (max %d chars)", maxNameLen)
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("profile name must be lowercase alphanumeric with hyphens (e.g. 'my-work')")
	}
	if reservedNames[name] {
		return fmt.Errorf("'%s' is a reserved name", name)
	}
	return nil
}

// validateSettingsJSON validates that the file at path is valid JSON
// and parses as an object (not array, string, etc.).
func validateSettingsJSON(path string) error {
	obj, err := loadJSON(path)
	if err != nil {
		return err
	}
	if obj == nil {
		return fmt.Errorf("invalid JSON in %s: must be an object, not null", path)
	}
	return nil
}
