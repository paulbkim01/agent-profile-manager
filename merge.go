package main

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
