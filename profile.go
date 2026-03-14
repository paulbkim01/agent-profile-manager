package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ProfileMeta is the profile.yaml structure.
type ProfileMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	CreatedAt   string `yaml:"created_at"`
	Source      string `yaml:"source,omitempty"`
}

// ProfileInfo holds profile metadata plus its filesystem path.
type ProfileInfo struct {
	Meta ProfileMeta
	Dir  string
}

// createProfile makes a new profile directory with optional source import.
// source: "" = empty, "current" = import from ~/.claude, "<name>" = copy from profile.
func createProfile(cfg *Config, name, source, description string) error {
	if err := validateProfileName(name); err != nil {
		return err
	}

	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("profile '%s' already exists", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking profile directory %s: %w", dir, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating profile directory %s: %w", dir, err)
	}

	// Create subdirs
	for _, sub := range managedDirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("creating subdirectory %s: %w", sub, err)
		}
	}

	// Handle source
	switch source {
	case "", "empty":
		// Write empty settings.json
		if err := os.WriteFile(filepath.Join(dir, settingsFile), []byte("{}\n"), 0o644); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("writing empty settings.json: %w", err)
		}
		log.Printf("profile: created empty profile '%s'", name)

	case "current":
		// Import from ~/.claude/
		if err := importFrom(cfg.ClaudeDir, dir); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("importing from current: %w", err)
		}
		log.Printf("profile: imported '%s' from %s", name, cfg.ClaudeDir)

	default:
		// Copy from another profile
		srcDir := cfg.ProfileDir(source)
		if _, err := os.Stat(srcDir); err != nil {
			os.RemoveAll(dir)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("source profile '%s' not found", source)
			}
			return fmt.Errorf("checking source profile directory %s: %w", srcDir, err)
		}
		if err := importFrom(srcDir, dir); err != nil {
			os.RemoveAll(dir)
			return fmt.Errorf("copying from '%s': %w", source, err)
		}
		log.Printf("profile: copied '%s' from profile '%s'", name, source)
	}

	// After importing settings.json from source, validate it
	settingsDst := filepath.Join(dir, settingsFile)
	if err := validateSettingsJSON(settingsDst); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("imported settings invalid: %w", err)
	}

	// Write profile.yaml
	meta := ProfileMeta{
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      source,
	}
	if err := writeMeta(dir, meta); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("writing profile metadata: %w", err)
	}
	return nil
}

// deleteProfile removes a profile. Checks if it's active first.
func deleteProfile(cfg *Config, name string, force bool) error {
	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile '%s' not found", name)
		}
		return fmt.Errorf("checking profile directory %s: %w", dir, err)
	}

	// Read default once for both the guard check and clearing
	defaultProfile, err := cfg.DefaultProfile()
	if err != nil {
		return fmt.Errorf("reading default profile: %w", err)
	}

	if !force {
		if defaultProfile == name {
			return fmt.Errorf("profile '%s' is the global default. Use --force to delete anyway", name)
		}
		if activeProfile := os.Getenv(envAPMProfile); activeProfile == name {
			return fmt.Errorf("profile '%s' is active in this shell. Use --force to delete anyway", name)
		}
	}

	// Clear default if this was it
	if defaultProfile == name {
		if err := cfg.ClearDefaultProfile(); err != nil {
			return fmt.Errorf("clearing default profile: %w", err)
		}
		log.Printf("profile: cleared global default (was '%s')", name)
	}

	// Remove generated dir
	genDir := cfg.GeneratedProfileDir(name)
	if err := os.RemoveAll(genDir); err != nil {
		return fmt.Errorf("removing generated directory for '%s': %w", name, err)
	}

	// Remove profile dir
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("deleting '%s': %w", name, err)
	}

	log.Printf("profile: deleted '%s'", name)
	return nil
}

// listProfiles returns all profiles.
func listProfiles(cfg *Config) ([]ProfileInfo, error) {
	entries, err := os.ReadDir(cfg.ProfilesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading profiles directory: %w", err)
	}

	var profiles []ProfileInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := readMeta(filepath.Join(cfg.ProfilesDir, e.Name()))
		if err != nil {
			// Skip directories without profile.yaml
			log.Printf("profile: skipping %s: %v", e.Name(), err)
			continue
		}
		profiles = append(profiles, ProfileInfo{
			Meta: meta,
			Dir:  filepath.Join(cfg.ProfilesDir, e.Name()),
		})
	}
	return profiles, nil
}

// getProfile returns a single profile's info.
func getProfile(cfg *Config, name string) (ProfileInfo, error) {
	dir := cfg.ProfileDir(name)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProfileInfo{}, fmt.Errorf("profile '%s' not found", name)
		}
		return ProfileInfo{}, fmt.Errorf("checking profile directory %s: %w", dir, err)
	}
	meta, err := readMeta(dir)
	if err != nil {
		return ProfileInfo{}, fmt.Errorf("reading profile '%s' metadata: %w", name, err)
	}
	return ProfileInfo{Meta: meta, Dir: dir}, nil
}

// profileExists checks if a profile exists.
// Returns false for both missing profiles and unexpected stat errors.
// Callers that need to distinguish should use getProfile() instead.
func profileExists(cfg *Config, name string) bool {
	_, err := os.Stat(cfg.ProfileDir(name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("profile: unexpected error checking %s: %v", name, err)
	}
	return err == nil
}

// importFrom copies settings.json and managed dirs from src to dst.
func importFrom(src, dst string) error {
	// Copy settings.json
	settingsSrc := filepath.Join(src, settingsFile)
	settingsDst := filepath.Join(dst, settingsFile)
	if data, err := os.ReadFile(settingsSrc); err == nil {
		if err := os.WriteFile(settingsDst, data, 0o644); err != nil {
			return fmt.Errorf("writing settings.json: %w", err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// No settings.json in source, write empty
		if err := os.WriteFile(settingsDst, []byte("{}\n"), 0o644); err != nil {
			return fmt.Errorf("writing empty settings.json: %w", err)
		}
	} else {
		return fmt.Errorf("reading source settings.json: %w", err)
	}

	// Copy managed dirs using shared copyDir
	for _, dir := range managedDirs {
		srcDir := filepath.Join(src, dir)
		dstDir := filepath.Join(dst, dir)
		if _, err := os.Stat(srcDir); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := copyDir(srcDir, dstDir); err != nil {
			return fmt.Errorf("copying %s directory: %w", dir, err)
		}
	}

	// Copy env file if present
	envSrc := filepath.Join(src, envFileName)
	if data, err := os.ReadFile(envSrc); err == nil {
		if err := os.WriteFile(filepath.Join(dst, envFileName), data, 0o644); err != nil {
			return fmt.Errorf("writing env file: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reading source env file: %w", err)
	}

	// Copy root-level extras (files/dirs not in managed set or profile meta)
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading source directory: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if managedItemSet[name] || name == profileMetaFile || name == settingsFile {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		if e.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("copying extra dir %s: %w", name, err)
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return fmt.Errorf("reading extra file %s: %w", name, err)
			}
			fi, _ := os.Stat(srcPath)
			if err := os.WriteFile(dstPath, data, fi.Mode()); err != nil {
				return fmt.Errorf("writing extra file %s: %w", name, err)
			}
		}
	}

	return nil
}

// seedRuntimeState copies all non-managed items from claudeDir to genDir.
// Used when creating a profile from current state to preserve runtime files
// (history, sessions, plugins, etc.) in the generated dir.
func seedRuntimeState(claudeDir, genDir string) error {
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", claudeDir, err)
	}
	for _, e := range entries {
		name := e.Name()
		// Skip managed items — they are generated from common + profile
		if managedItemSet[name] {
			continue
		}
		// Skip profile metadata
		if name == profileMetaFile {
			continue
		}
		src := filepath.Join(claudeDir, name)
		dst := filepath.Join(genDir, name)
		// Skip if already exists in genDir
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		fi, err := os.Lstat(src)
		if err != nil {
			continue
		}
		if fi.IsDir() {
			if err := copyDir(src, dst); err != nil {
				return fmt.Errorf("copying %s: %w", name, err)
			}
			log.Printf("seed: copied dir %s", name)
		} else if isSymlink(fi) {
			link, err := os.Readlink(src)
			if err != nil {
				return fmt.Errorf("reading symlink %s: %w", src, err)
			}
			if err := os.Symlink(link, dst); err != nil {
				return fmt.Errorf("creating symlink %s: %w", dst, err)
			}
			log.Printf("seed: symlinked %s", name)
		} else {
			data, err := os.ReadFile(src)
			if err != nil {
				return fmt.Errorf("reading %s: %w", src, err)
			}
			if err := os.WriteFile(dst, data, fi.Mode()); err != nil {
				return fmt.Errorf("writing %s: %w", dst, err)
			}
			log.Printf("seed: copied file %s", name)
		}
	}
	return nil
}

func writeMeta(dir string, meta ProfileMeta) error {
	data, err := yaml.Marshal(&meta)
	if err != nil {
		return fmt.Errorf("marshaling profile metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, profileMetaFile), data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", profileMetaFile, err)
	}
	return nil
}

func readMeta(dir string) (ProfileMeta, error) {
	data, err := os.ReadFile(filepath.Join(dir, profileMetaFile))
	if err != nil {
		return ProfileMeta{}, fmt.Errorf("reading %s in %s: %w", profileMetaFile, dir, err)
	}
	var meta ProfileMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return ProfileMeta{}, fmt.Errorf("parsing %s in %s: %w", profileMetaFile, dir, err)
	}
	return meta, nil
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

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

// levenshtein computes case-insensitive edit distance between two strings.
func levenshtein(s, t string) int {
	s, t = strings.ToLower(s), strings.ToLower(t)
	d := make([][]int, len(s)+1)
	for i := range d {
		d[i] = make([]int, len(t)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for j := 1; j <= len(t); j++ {
		for i := 1; i <= len(s); i++ {
			if s[i-1] == t[j-1] {
				d[i][j] = d[i-1][j-1]
			} else {
				m := d[i-1][j]
				if d[i][j-1] < m {
					m = d[i][j-1]
				}
				if d[i-1][j-1] < m {
					m = d[i-1][j-1]
				}
				d[i][j] = m + 1
			}
		}
	}
	return d[len(s)][len(t)]
}

// profileNotFoundError returns a standard "not found" error with a suggestion hint.
func profileNotFoundError(cfg *Config, name string) error {
	return fmt.Errorf("profile '%s' not found%s\nRun 'apm ls' to see available profiles", name, suggestProfile(cfg, name))
}

// requireProfile validates the name and checks that the profile exists.
func requireProfile(cfg *Config, name string) error {
	if err := validateProfileName(name); err != nil {
		return fmt.Errorf("invalid profile name: %w", err)
	}
	if !profileExists(cfg, name) {
		return profileNotFoundError(cfg, name)
	}
	return nil
}

// suggestProfile returns a "Did you mean 'X'?" hint if a close match exists.
// Returns empty string if no match is within edit distance 2.
func suggestProfile(cfg *Config, typed string) string {
	profiles, err := listProfiles(cfg)
	if err != nil || len(profiles) == 0 {
		return ""
	}
	var best string
	bestDist := 3 // threshold: max edit distance 2
	for _, p := range profiles {
		d := levenshtein(typed, p.Meta.Name)
		if d < bestDist {
			best = p.Meta.Name
			bestDist = d
		}
	}
	if best != "" {
		return fmt.Sprintf("\n\nDid you mean this?\n\t%s", best)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Settings merge
// ---------------------------------------------------------------------------

// unionArrayKeys are JSON paths where arrays should be unioned instead of replaced.
var unionArrayKeys = map[string]bool{
	"permissions.allow": true,
}

// objectMergeKeys are JSON paths where the value is treated as an object merge
// even if it looks like it could be something else.
var objectMergeKeys = map[string]bool{
	"enabledPlugins": true,
}

// mergeSettings deep-merges common and profile settings.
// Profile values override common. Null values delete keys.
func mergeSettings(common, profile map[string]any) map[string]any {
	result := make(map[string]any)
	deepMerge(result, common, "")
	deepMerge(result, profile, "")
	applyDeletions(result, profile, "")
	return result
}

// deepMerge recursively merges src into dst.
func deepMerge(dst, src map[string]any, prefix string) {
	for key, srcVal := range src {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		// Null sentinel: skip during merge, handled by applyDeletions
		if srcVal == nil {
			continue
		}

		dstVal, exists := dst[key]

		// Special case: union arrays (permissions.allow)
		if unionArrayKeys[path] {
			srcArr, srcOk := toSlice(srcVal)
			dstArr, dstOk := toSlice(dstVal)
			if srcOk {
				if dstOk {
					dst[key] = unionStrings(dstArr, srcArr)
				} else {
					dst[key] = srcVal
				}
				merged := dst[key].([]any)
				log.Printf("merge: union %s (%d items)", path, len(merged))
				continue
			}
		}

		// Special case: object merge keys (enabledPlugins)
		if objectMergeKeys[path] || objectMergeKeys[key] {
			srcMap, srcOk := srcVal.(map[string]any)
			dstMap, dstOk := dstVal.(map[string]any)
			if srcOk && dstOk {
				// Clone dstMap to avoid mutating the caller's input map
				merged := make(map[string]any, len(dstMap)+len(srcMap))
				for k, v := range dstMap {
					merged[k] = v
				}
				for k, v := range srcMap {
					merged[k] = v
				}
				dst[key] = merged
				log.Printf("merge: object-merge %s", path)
				continue
			} else if srcOk && !exists {
				dst[key] = srcVal
				continue
			}
		}

		// Both are objects: recursive merge
		srcMap, srcIsMap := srcVal.(map[string]any)
		dstMap, dstIsMap := dstVal.(map[string]any)
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap, path)
			log.Printf("merge: recurse into %s", path)
			continue
		}

		// Default: profile wins (scalars, arrays, type mismatches)
		// Clone maps to avoid aliasing the caller's input — without this,
		// nested maps from common end up in result by reference, and the
		// second deepMerge pass (for profile) mutates them.
		dst[key] = cloneValue(srcVal)
		if exists {
			log.Printf("merge: override %s", path)
		} else {
			log.Printf("merge: set %s", path)
		}
	}
}

// applyDeletions removes keys where profile has null.
func applyDeletions(dst, profile map[string]any, prefix string) {
	for key, val := range profile {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if val == nil {
			delete(dst, key)
			log.Printf("merge: delete %s (null sentinel)", path)
			continue
		}

		// Recurse into nested objects
		profileMap, pOk := val.(map[string]any)
		dstMap, dOk := dst[key].(map[string]any)
		if pOk && dOk {
			applyDeletions(dstMap, profileMap, path)
		}
	}
}

// cloneValue returns a deep copy of v if it is a map or slice,
// preventing aliasing between merge inputs and the result.
func cloneValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return cloneMap(val)
	case []any:
		c := make([]any, len(val))
		for i, item := range val {
			c[i] = cloneValue(item)
		}
		return c
	default:
		return v
	}
}

// cloneMap returns a deep copy of a map[string]any.
func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = cloneValue(v)
	}
	return c
}

// toSlice converts a JSON array ([]any) to check if it's an array.
// Returns the original []any and true if it is an array.
func toSlice(v any) ([]any, bool) {
	arr, ok := v.([]any)
	return arr, ok
}

// unionStrings merges two []any slices, deduplicating by string value.
func unionStrings(a, b []any) []any {
	seen := make(map[string]bool)
	result := make([]any, 0, len(a)+len(b))
	for _, v := range a {
		s := fmt.Sprintf("%v", v)
		if !seen[s] {
			seen[s] = true
			result = append(result, v)
		}
	}
	for _, v := range b {
		s := fmt.Sprintf("%v", v)
		if !seen[s] {
			seen[s] = true
			result = append(result, v)
		}
	}
	return result
}

// loadJSON reads a JSON file into map[string]any.
// Returns empty map if file doesn't exist.
func loadJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return result, nil
}

// writeJSON writes map[string]any to a JSON file with indentation.
func writeJSON(path string, data map[string]any) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON for %s: %w", path, err)
	}
	out = append(out, '\n')
	return atomicWriteFile(path, out, 0o644)
}

// ---------------------------------------------------------------------------
// Environment files
// ---------------------------------------------------------------------------

// envKeyPattern matches valid POSIX environment variable names.
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// parseEnvFile reads a KEY=VALUE file, skipping blank lines and comments.
// Values may optionally be single- or double-quoted.
func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: invalid line (expected KEY=VALUE): %s", path, lineNum, line)
		}
		key = strings.TrimSpace(key)
		if !envKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("%s:%d: invalid env var name %q (must match [A-Za-z_][A-Za-z0-9_]*)", path, lineNum, key)
		}
		val = strings.TrimSpace(val)
		// Strip optional quotes and handle escape sequences
		if len(val) >= 2 {
			if val[0] == '"' && val[len(val)-1] == '"' {
				val = val[1 : len(val)-1]
				val = unescapeDoubleQuoted(val)
			} else if val[0] == '\'' && val[len(val)-1] == '\'' {
				val = val[1 : len(val)-1]
			}
		}
		vars[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return vars, nil
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// mergeEnvFiles merges common/env + profile/env and writes the result to genDir/env.
// Profile values override common values.
func mergeEnvFiles(commonDir, profileDir, genDir string) error {
	commonVars, err := parseEnvFile(filepath.Join(commonDir, envFileName))
	if err != nil {
		return fmt.Errorf("common env: %w", err)
	}
	profileVars, err := parseEnvFile(filepath.Join(profileDir, envFileName))
	if err != nil {
		return fmt.Errorf("profile env: %w", err)
	}

	// Nothing to merge
	if len(commonVars) == 0 && len(profileVars) == 0 {
		return nil
	}

	// Merge: profile wins
	merged := make(map[string]string)
	for k, v := range commonVars {
		merged[k] = v
	}
	for k, v := range profileVars {
		merged[k] = v
	}

	log.Printf("generate: merged env (%d common + %d profile = %d total)",
		len(commonVars), len(profileVars), len(merged))

	return writeEnvFile(filepath.Join(genDir, envFileName), merged)
}

// writeEnvFile writes a sorted KEY="VALUE" file with proper escaping.
func writeEnvFile(path string, vars map[string]string) error {
	var b strings.Builder
	b.WriteString("# Generated by apm — do not edit\n")
	for _, k := range sortedKeys(vars) {
		fmt.Fprintf(&b, "%s=\"%s\"\n", k, escapeDoubleQuoted(vars[k]))
	}
	return atomicWriteFile(path, []byte(b.String()), 0o644)
}

// escapeDoubleQuoted escapes a value for safe embedding inside double quotes.
// Handles backslash, double-quote, dollar sign, backtick (to prevent shell
// expansion if the file is ever sourced), and newlines.
func escapeDoubleQuoted(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "$", `\$`)
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// unescapeDoubleQuoted reverses escapeDoubleQuoted.
func unescapeDoubleQuoted(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\$`, "$")
	s = strings.ReplaceAll(s, "\\`", "`")
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

// envShellLines reads the generated env file and returns formatted shell
// statements for each variable. The formatter receives (key, value) pairs.
func envShellLines(genDir string, format func(k, v string) string) ([]string, error) {
	vars, err := parseEnvFile(filepath.Join(genDir, envFileName))
	if err != nil {
		return nil, err
	}
	if len(vars) == 0 {
		return nil, nil
	}
	lines := make([]string, 0, len(vars))
	for _, k := range sortedKeys(vars) {
		lines = append(lines, format(k, vars[k]))
	}
	return lines, nil
}

// readEnvExports reads the generated env file and returns shell export statements.
func readEnvExports(genDir string) ([]string, error) {
	return envShellLines(genDir, func(k, v string) string {
		return fmt.Sprintf("export %s='%s'", k, shellQuote(v))
	})
}

// readEnvUnsets reads the generated env file and returns shell unset statements.
func readEnvUnsets(genDir string) ([]string, error) {
	return envShellLines(genDir, func(k, _ string) string {
		return fmt.Sprintf("unset %s", k)
	})
}

// ---------------------------------------------------------------------------
// Input hashing (staleness detection)
// ---------------------------------------------------------------------------

// computeInputHash computes a SHA-256 hash of a manifest of source file
// metadata (names + mtimes) for a profile's inputs. This includes:
//   - common/settings.json, profile/settings.json
//   - common/env, profile/env
//   - directory listings of managed dirs (skills/, commands/, agents/, rules/)
//   - extra files from common/ and profile/ (non-managed, non-meta)
//
// Uses os.Lstat (no file reads) so it's fast (~1-5ms).
// Iteration order is deterministic (hardcoded dir order + os.ReadDir sorts by name).
func computeInputHash(cfg *Config, profileName string) (string, error) {
	profileDir := cfg.ProfileDir(profileName)
	sourceDirs := []string{cfg.CommonDir, profileDir}

	var entries []string

	// Hash settings.json and env files
	for _, name := range []string{settingsFile, envFileName} {
		for _, dir := range sourceDirs {
			if entry, err := statEntry(filepath.Join(dir, name)); err == nil {
				entries = append(entries, entry)
			}
		}
	}

	// Hash managed directory listings
	for _, dirName := range managedDirs {
		for _, parent := range sourceDirs {
			dir := filepath.Join(parent, dirName)
			dirEntries, err := os.ReadDir(dir)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return "", fmt.Errorf("reading %s: %w", dir, err)
			}
			for _, e := range dirEntries {
				if entry, err := statEntry(filepath.Join(dir, e.Name())); err == nil {
					entries = append(entries, entry)
				}
			}
		}
	}

	// Hash extra files from common and profile
	for _, dir := range sourceDirs {
		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("reading %s: %w", dir, err)
		}
		for _, e := range dirEntries {
			name := e.Name()
			if managedItemSet[name] || name == profileMetaFile {
				continue
			}
			if entry, err := statEntry(filepath.Join(dir, name)); err == nil {
				entries = append(entries, entry)
			}
		}
	}

	manifest := strings.Join(entries, "\n")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(manifest)))
	log.Printf("input_hash: computed %s for %s (%d entries)", hash[:12], profileName, len(entries))
	return hash, nil
}

// statEntry returns a manifest line "path:mtime_unix" for a file using Lstat.
func statEntry(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", path, fi.ModTime().UnixNano()), nil
}

// isProfileStale compares the computed input hash against the stored one.
// Returns (stale, computedHash, error). The computedHash is returned so
// callers can pass it to writeMetaHash without recomputing.
func isProfileStale(cfg *Config, profileName string) (bool, string, error) {
	genDir := cfg.GeneratedProfileDir(profileName)

	computed, err := computeInputHash(cfg, profileName)
	if err != nil {
		return false, "", fmt.Errorf("computing input hash for %s: %w", profileName, err)
	}

	stored, err := readStoredInputHash(genDir)
	if err != nil {
		log.Printf("input_hash: cannot read stored hash for %s: %v", profileName, err)
		return true, computed, nil // missing or corrupt meta = stale
	}

	stale := computed != stored
	if stale {
		log.Printf("input_hash: %s is stale (stored=%s computed=%s)", profileName, stored[:min(12, len(stored))], computed[:min(12, len(computed))])
	}
	return stale, computed, nil
}

// readStoredInputHash reads the input_hash field from .apm-meta.json.
func readStoredInputHash(genDir string) (string, error) {
	meta, err := readMetaJSON(genDir)
	if err != nil {
		return "", err
	}
	hash, ok := meta[metaKeyInputHash]
	if !ok || hash == "" {
		return "", fmt.Errorf("no input_hash in meta")
	}
	return hash, nil
}

// ---------------------------------------------------------------------------
// Profile generation
// ---------------------------------------------------------------------------

// cleanManagedItems removes only managed items and root-level symlinks from genDir.
// Runtime state (files/dirs created by Claude CLI) is preserved.
func cleanManagedItems(genDir string) error {
	// Remove items in managedItemSet
	for name := range managedItemSet {
		path := filepath.Join(genDir, name)
		if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing managed item %s: %w", name, err)
		}
	}

	// Remove root-level symlinks only (extras from common/profile).
	// Runtime-created regular files/dirs are preserved.
	entries, err := os.ReadDir(genDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading generated dir: %w", err)
	}
	for _, e := range entries {
		path := filepath.Join(genDir, e.Name())
		fi, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if isSymlink(fi) {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("removing symlink %s: %w", path, err)
			}
			log.Printf("generate: removed symlink %s", e.Name())
		}
	}

	return nil
}

// generateProfile builds the generated directory for a profile.
// Preserves runtime state (files created by Claude CLI) across rebuilds.
// Only managed items (settings, skills, commands, agents, rules, meta)
// and root-level symlinks are cleaned before regeneration.
func generateProfile(cfg *Config, name string) error {
	return generateProfileWithHash(cfg, name, "")
}

// generateProfileWithHash is like generateProfile but accepts a precomputed
// input hash to avoid recomputing it (used by ensureFresh which already computed it).
func generateProfileWithHash(cfg *Config, name, inputHash string) error {
	genDir := cfg.GeneratedProfileDir(name)
	profileDir := cfg.ProfileDir(name)

	// Verify profile exists
	if _, err := os.Stat(profileDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile '%s' not found", name)
		}
		return fmt.Errorf("checking profile directory: %w", err)
	}

	// Clean only managed items, preserving runtime state
	if err := cleanManagedItems(genDir); err != nil {
		return fmt.Errorf("cleaning managed items: %w", err)
	}
	if err := os.MkdirAll(genDir, 0o755); err != nil {
		return fmt.Errorf("creating generated dir: %w", err)
	}

	log.Printf("generate: building %s", genDir)

	// Step 1+2: Load, validate, and deep merge settings.json
	if err := mergeProfileSettings(cfg, profileDir, genDir); err != nil {
		return fmt.Errorf("merging settings: %w", err)
	}

	// Step 3: Merge managed directories (skills/, commands/, agents/)
	for _, dir := range managedDirs {
		if err := mergeDir(cfg, profileDir, genDir, dir); err != nil {
			return fmt.Errorf("merging %s: %w", dir, err)
		}
	}

	// Step 4: Merge env files (common/env + profile/env)
	if err := mergeEnvFiles(cfg.CommonDir, profileDir, genDir); err != nil {
		return fmt.Errorf("merging env: %w", err)
	}

	// Step 5: Symlink extra files from common + profile (profile wins)
	if err := symlinkExtras(cfg.CommonDir, profileDir, genDir); err != nil {
		return fmt.Errorf("symlinking extras: %w", err)
	}

	// Step 6: Write meta hash for staleness detection
	if err := writeMetaHash(cfg, name, inputHash); err != nil {
		return fmt.Errorf("writing meta hash: %w", err)
	}

	return nil
}

// mergeProfileSettings loads, validates, and deep-merges common + profile settings.
func mergeProfileSettings(cfg *Config, profileDir, genDir string) error {
	commonSettings, err := loadJSON(filepath.Join(cfg.CommonDir, settingsFile))
	if err != nil {
		return fmt.Errorf("common settings: %w", err)
	}
	profileSettings, err := loadJSON(filepath.Join(profileDir, settingsFile))
	if err != nil {
		return fmt.Errorf("profile settings: %w", err)
	}

	merged := mergeSettings(commonSettings, profileSettings)
	log.Printf("generate: merged settings (%d common keys + %d profile keys)", len(commonSettings), len(profileSettings))

	return writeJSON(filepath.Join(genDir, settingsFile), merged)
}

// mergeDir creates a directory with symlinks from both common and profile sources.
// Profile entries override common entries with the same name.
func mergeDir(cfg *Config, profileDir, genDir, dirName string) error {
	targetDir := filepath.Join(genDir, dirName)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating target dir %s: %w", targetDir, err)
	}

	commonSrc := filepath.Join(cfg.CommonDir, dirName)
	profileSrc := filepath.Join(profileDir, dirName)

	linked := make(map[string]bool)

	// Profile entries first (they win on conflict)
	if err := symlinkEntries(profileSrc, targetDir, dirName, "profile", linked); err != nil {
		return err
	}

	// Common entries (skip if profile already has same name)
	if err := symlinkEntries(commonSrc, targetDir, dirName, "common", linked); err != nil {
		return err
	}

	return nil
}

// symlinkEntries reads entries from srcDir and creates symlinks in targetDir.
// Already-linked names in the linked map are skipped (profile wins over common).
// New entries are recorded in linked. label is used for log messages.
func symlinkEntries(srcDir, targetDir, dirName, label string, linked map[string]bool) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s dir %s: %w", label, srcDir, err)
	}
	for _, e := range entries {
		if linked[e.Name()] {
			log.Printf("generate: skip %s/%s (overridden by profile)", dirName, e.Name())
			continue
		}

		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(targetDir, e.Name())

		// Resolve the actual target if src is itself a symlink
		realSrc, err := filepath.EvalSymlinks(src)
		if err != nil {
			realSrc = src
		}

		if err := os.Symlink(realSrc, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, realSrc, err)
		}
		log.Printf("generate: link %s/%s -> %s", dirName, e.Name(), label)
		linked[e.Name()] = true
	}
	return nil
}

// symlinkExtras links any files/dirs from common and profile that aren't
// already handled by settings.json merge or managed dir merge.
// Profile entries override common entries with the same name.
// Nothing is linked from the backup — generated = common + profile only.
func symlinkExtras(commonDir, profileDir, genDir string) error {
	linked := make(map[string]bool)

	// Profile extras first (they win on conflict)
	if err := linkExtrasFrom(profileDir, genDir, "profile", linked); err != nil {
		return err
	}

	// Common extras (skip if profile already provided)
	if err := linkExtrasFrom(commonDir, genDir, "common", linked); err != nil {
		return err
	}

	return nil
}

// linkExtrasFrom symlinks files/dirs from srcDir into genDir, skipping
// managed items and anything already present in genDir.
func linkExtrasFrom(srcDir, genDir, label string, linked map[string]bool) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s dir: %w", label, err)
	}

	for _, e := range entries {
		name := e.Name()

		// Skip managed items (already handled)
		if managedItemSet[name] {
			continue
		}

		// Skip profile metadata
		if name == profileMetaFile {
			continue
		}

		// Skip if already linked by a higher-priority source
		if linked[name] {
			continue
		}

		dst := filepath.Join(genDir, name)

		// Skip if already exists in generated dir
		if _, err := os.Lstat(dst); err == nil {
			continue
		}

		src := filepath.Join(srcDir, name)
		realSrc, err := filepath.EvalSymlinks(src)
		if err != nil {
			realSrc = src
		}

		if err := os.Symlink(realSrc, dst); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", dst, realSrc, err)
		}
		log.Printf("generate: link %s -> %s", name, label)
		linked[name] = true
	}

	return nil
}

// readMetaJSON reads and parses .apm-meta.json from a generated dir.
func readMetaJSON(genDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(genDir, apmMetaFile))
	if err != nil {
		return nil, err
	}
	var meta map[string]string
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", apmMetaFile, err)
	}
	return meta, nil
}

// writeMetaHash writes .apm-meta.json with the input hash for staleness detection.
// If inputHash is empty, it computes one from the current source files.
func writeMetaHash(cfg *Config, profileName, inputHash string) error {
	genDir := cfg.GeneratedProfileDir(profileName)

	if inputHash == "" {
		var err error
		inputHash, err = computeInputHash(cfg, profileName)
		if err != nil {
			log.Printf("generate: warn: cannot compute input hash: %v", err)
		}
	}

	meta := map[string]string{metaKeyInputHash: inputHash}
	out, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshaling meta hash: %w", err)
	}
	if err := atomicWriteFile(filepath.Join(genDir, apmMetaFile), out, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", apmMetaFile, err)
	}
	return nil
}

// ensureFresh checks if a profile's generated output is stale and regenerates if needed.
func ensureFresh(cfg *Config, name string) error {
	stale, computedHash, err := isProfileStale(cfg, name)
	if err != nil {
		return fmt.Errorf("checking staleness for %s: %w", name, err)
	}
	if stale {
		log.Printf("ensureFresh: %s is stale, regenerating", name)
		return generateProfileWithHash(cfg, name, computedHash)
	}

	log.Printf("ensureFresh: %s is fresh", name)
	return nil
}

// ---------------------------------------------------------------------------
// Activation, nuke, and migration
// ---------------------------------------------------------------------------

// activateProfile ensures the generated directory is up-to-date.
// Activation is completed by the caller setting CLAUDE_CONFIG_DIR
// to the generated dir via shell env vars (not persisted to config).
func activateProfile(cfg *Config, name string) error {
	// Auto-regenerate if inputs changed since last generation
	if err := ensureFresh(cfg, name); err != nil {
		return fmt.Errorf("ensuring fresh profile: %w", err)
	}

	return nil
}

// deactivateProfile is a no-op. Profile deactivation is handled by
// unsetting env vars (APM_PROFILE, CLAUDE_CONFIG_DIR) in the shell.
// Generated profile directories are preserved on disk for future reactivation.
func deactivateProfile(cfg *Config) error {
	return nil
}

// copyClaudeJSON copies ~/.claude.json into the generated profile dir.
// Used for --current imports so the profile inherits the current auth state.
// Skips gracefully if ~/.claude.json doesn't exist.
func copyClaudeJSON(genDir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	src := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("activate: no ~/.claude.json to copy")
			return nil
		}
		return fmt.Errorf("reading %s: %w", src, err)
	}
	dst := filepath.Join(genDir, ".claude.json")
	if err := atomicWriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	log.Printf("activate: copied ~/.claude.json to %s", dst)
	return nil
}

// flattenGeneratedDir copies a generated profile dir into claudePath,
// resolving symlinks. Also copies .claude.json to ~/.claude.json.
// Skips if claudePath already exists as a real directory.
func flattenGeneratedDir(cfg *Config, profileName, claudePath string) error {
	genDir := cfg.GeneratedProfileDir(profileName)
	if _, statErr := os.Stat(genDir); statErr != nil {
		return nil // generated dir doesn't exist, nothing to flatten
	}

	// Check if ~/.claude already exists as a real directory
	fi, lstatErr := os.Lstat(claudePath)
	if lstatErr == nil {
		if isSymlink(fi) {
			os.Remove(claudePath)
		} else if fi.IsDir() {
			log.Printf("nuke: %s is a real directory, skipping flatten", claudePath)
			return nil
		}
	}

	// Copy generated dir contents to ~/.claude
	if err := copyDirFlat(genDir, claudePath); err != nil {
		return fmt.Errorf("flattening %s to %s: %w", genDir, claudePath, err)
	}

	// Copy .claude.json from generated dir to ~/.claude.json
	genJSON := filepath.Join(genDir, ".claude.json")
	if data, readErr := os.ReadFile(genJSON); readErr == nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			log.Printf("nuke: warning: cannot resolve home dir for .claude.json: %v", homeErr)
		} else if err := atomicWriteFile(filepath.Join(home, ".claude.json"), data, 0o600); err != nil {
			log.Printf("nuke: warning: failed to restore .claude.json: %v", err)
		}
	}

	// Remove APM-specific artifacts from flattened dir
	os.Remove(filepath.Join(claudePath, apmMetaFile))
	os.Remove(filepath.Join(claudePath, ".claude.json"))

	return nil
}

// nukeAPM removes all profiles and generated data while preserving the
// common directory. If there's an active profile, the generated dir is
// flattened into ~/.claude so auth tokens and runtime state survive.
func nukeAPM(cfg *Config) error {
	claudePath := cfg.ClaudeDir

	cf, cfErr := cfg.readConfigFile()

	// If there's an active profile, flatten its generated dir into ~/.claude.
	// Check env var first (current shell), then config (legacy backwards-compat).
	flattenProfile := os.Getenv(envAPMProfile)
	if flattenProfile == "" && cfErr == nil {
		flattenProfile = cf.ActiveProfile
	}
	if flattenProfile != "" {
		if err := flattenGeneratedDir(cfg, flattenProfile, claudePath); err != nil {
			return err
		}
	}

	// Unlink all symlinks then remove generated and profiles directories.
	// Unlinking first prevents RemoveAll from following symlinks into external dirs.
	for _, dir := range []string{cfg.GeneratedDir, cfg.ProfilesDir} {
		if err := unlinkAll(dir); err != nil {
			return fmt.Errorf("unlinking symlinks in %s: %w", dir, err)
		}
		log.Printf("nuke: removing %s", dir)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("removing %s: %w", dir, err)
		}
	}

	// Clear profile-related config entries
	if cfErr == nil {
		cf.ActiveProfile = ""
		cf.DefaultProfile = ""
		_ = cfg.writeConfigFile(cf)
	}

	return nil
}

// copyDir recursively copies src to dst, preserving symlinks.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// Use Lstat to detect symlinks
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}

		if isSymlink(info) {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// copyDirFlat recursively copies src to dst, resolving symlinks to real files.
// Unlike copyDir, this follows symlinks (using os.Stat) so the result is a
// self-contained directory with no symlinks that could become dangling.
func copyDirFlat(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// Use Stat (not Lstat) to follow symlinks
		info, err := os.Stat(path)
		if err != nil {
			// Dangling symlink — skip with warning
			if errors.Is(err, os.ErrNotExist) {
				log.Printf("copyDirFlat: skipping dangling symlink %s", path)
				return nil
			}
			return err
		}

		if info.IsDir() {
			if err := os.MkdirAll(target, info.Mode()); err != nil {
				return err
			}
			// WalkDir does not descend into symlinked dirs, so recurse manually.
			// Resolve the symlink to get the real path for WalkDir to traverse.
			if d.Type()&os.ModeSymlink != 0 {
				resolved, err := filepath.EvalSymlinks(path)
				if err != nil {
					return fmt.Errorf("resolving symlink %s: %w", path, err)
				}
				return copyDirFlat(resolved, target)
			}
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// unlinkAll removes all symlinks within dir so that a subsequent
// os.RemoveAll cannot follow symlinks into external directories.
func unlinkAll(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		info, lErr := os.Lstat(path)
		if lErr != nil {
			return nil
		}
		if isSymlink(info) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("unlinking %s: %w", path, err)
			}
		}
		return nil
	})
}

// migrateFromSymlinks performs a one-time migration from the old symlink-based
// activation model to the new CLAUDE_CONFIG_DIR model.
func migrateFromSymlinks(cfg *Config) error {
	markerPath := filepath.Join(cfg.APMDir, ".migrated-v2")
	if _, err := os.Stat(markerPath); err == nil {
		return nil // already migrated
	}

	// Only run if APMDir exists (not a fresh install)
	if _, err := os.Stat(cfg.APMDir); errors.Is(err, os.ErrNotExist) {
		return nil
	}

	log.Printf("migrate: running one-time migration from symlinks to CLAUDE_CONFIG_DIR")

	// 1. If ~/.claude is a symlink, remove it and restore backup if available
	claudePath, err := defaultClaudeDir()
	if err != nil {
		return err
	}
	fi, err := os.Lstat(claudePath)
	if err == nil && isSymlink(fi) {
		log.Printf("migrate: removing ~/.claude symlink")
		if err := os.Remove(claudePath); err != nil {
			log.Printf("migrate: warning: failed to remove symlink: %v", err)
		}

		// Restore from backup if available
		backupDir := filepath.Join(cfg.APMDir, "claude-home")
		if _, statErr := os.Stat(backupDir); statErr == nil {
			log.Printf("migrate: restoring backup from %s", backupDir)
			if err := os.Rename(backupDir, claudePath); err != nil {
				log.Printf("migrate: warning: failed to restore backup: %v", err)
			}
		}
	}

	// 2. Migrate profiles/<name>/external/claude.json → generated/<name>/.claude.json
	profiles, _ := os.ReadDir(cfg.ProfilesDir)
	for _, p := range profiles {
		if !p.IsDir() {
			continue
		}
		extJSON := filepath.Join(cfg.ProfilesDir, p.Name(), "external", "claude.json")
		if data, readErr := os.ReadFile(extJSON); readErr == nil {
			genDir := cfg.GeneratedProfileDir(p.Name())
			if err := os.MkdirAll(genDir, 0o755); err == nil {
				dst := filepath.Join(genDir, ".claude.json")
				if _, statErr := os.Stat(dst); errors.Is(statErr, os.ErrNotExist) {
					os.WriteFile(dst, data, 0o600)
					log.Printf("migrate: copied external state for %s", p.Name())
				}
			}
		}
	}

	// 3. Clean up legacy directories (only if not used as claude_dir)
	cf, _ := cfg.readConfigFile()
	claudeHomeDir := filepath.Join(cfg.APMDir, "claude-home")
	if cf == nil || cf.ClaudeDir != claudeHomeDir {
		os.RemoveAll(claudeHomeDir)
	}
	os.RemoveAll(filepath.Join(cfg.APMDir, "claude-home-external"))
	for _, p := range profiles {
		if !p.IsDir() {
			continue
		}
		os.RemoveAll(filepath.Join(cfg.ProfilesDir, p.Name(), "external"))
	}

	// 4. Clear stale claude_home from config.yaml (read raw YAML to handle legacy field)
	data, err := os.ReadFile(cfg.ConfigPath)
	if err == nil {
		var raw map[string]interface{}
		if yaml.Unmarshal(data, &raw) == nil {
			if _, ok := raw["claude_home"]; ok {
				delete(raw, "claude_home")
				if out, mErr := yaml.Marshal(raw); mErr == nil {
					atomicWriteFile(cfg.ConfigPath, out, 0o644)
				}
			}
		}
	}

	// 5. Write migration marker
	os.MkdirAll(cfg.APMDir, 0o755)
	os.WriteFile(markerPath, []byte("migrated\n"), 0o644)

	log.Printf("migrate: migration complete")
	return nil
}
