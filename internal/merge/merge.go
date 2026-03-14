package merge

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
)

// unionArrayKeys are JSON paths where arrays should be unioned instead of replaced.
var unionArrayKeys = map[string]bool{
	"permissions.allow": true,
}

// objectMergeKeys are JSON paths where the value is treated as an object merge
// even if it looks like it could be something else.
var objectMergeKeys = map[string]bool{
	"enabledPlugins": true,
}

// Settings deep-merges common and profile settings.
// Profile values override common. Null values delete keys.
func Settings(common, profile map[string]any) map[string]any {
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
				for k, v := range srcMap {
					dstMap[k] = v
				}
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
		dst[key] = srcVal
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

// LoadJSON reads a JSON file into map[string]any.
// Returns empty map if file doesn't exist.
func LoadJSON(path string) (map[string]any, error) {
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

// WriteJSON writes map[string]any to a JSON file with indentation.
// Uses atomic write (temp file + rename).
func WriteJSON(path string, data map[string]any) error {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON for %s: %w", path, err)
	}
	out = append(out, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("writing temp file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming %s to %s: %w", tmp, path, err)
	}
	return nil
}
