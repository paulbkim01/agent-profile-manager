# Flatten Project Structure

## Problem

The project has 26 Go files spread across `cmd/` (10 commands) and `internal/` (6 sub-packages). Each `internal/` sub-package contains a single file. For a ~4,200 line CLI tool, this nesting adds navigation overhead without providing meaningful encapsulation.

## Decision

Flatten everything into `package main` at the project root. No sub-directories for Go source.

## Target Structure

```
main.go                  entry point
cli_commands.go          all cobra command definitions
config.go               Config struct, Load, Set/GetDefaultProfile
constants.go            ManagedItems, ManagedDirs, ReservedNames, path helpers
generate.go             profile dir generation
merge.go                deep JSON merge, LoadJSON, WriteJSON
profile.go              profile CRUD, importFrom
shell.go                bash/zsh init scripts
validate.go             name and settings validation

cli_commands_test.go    consolidated command tests
config_test.go
generate_test.go
merge_test.go
profile_test.go
shell_test.go
validate_test.go
```

16 files total (9 source + 7 test), down from 26.

## Name Collision Resolution

Everything is now in one package. Rename generic names with domain prefixes:

| Before (package-qualified) | After (flat) |
|---|---|
| `profile.Create()` | `createProfile()` |
| `profile.Delete()` | `deleteProfile()` |
| `profile.List()` | `listProfiles()` |
| `profile.Get()` | `getProfile()` |
| `profile.Exists()` | `profileExists()` |
| `profile.Meta` | `ProfileMeta` |
| `profile.Info` | `ProfileInfo` |
| `generate.Profile()` | `generateProfile()` |
| `merge.Settings()` | `mergeSettings()` |
| `merge.LoadJSON()` | `loadJSON()` |
| `merge.WriteJSON()` | `writeJSON()` |
| `validate.ProfileName()` | `validateProfileName()` |
| `validate.SettingsJSON()` | `validateSettingsJSON()` |
| `shell.InitScript()` | `shellInitScript()` |
| `config.Load()` | `loadConfig()` |
| `config.Config` | `Config` (no change needed) |
| `config.ConfigFile` | `ConfigFile` (no change needed) |
| `internal.DefaultAPMDir()` | `defaultAPMDir()` |
| `internal.DefaultClaudeDir()` | `defaultClaudeDir()` |
| `internal.ManagedItems` | `managedItems` |
| `internal.ManagedDirs` | `managedDirs` |
| `internal.ReservedNames` | `reservedNames` |
| `cmd.Execute()` | `rootCmd.Execute()` inlined in main |
| `cmd.IsNoActiveProfile()` | `isNoActiveProfile()` |

**generate.go internal collision:** `generate.go` has a private `mergeSettings()` helper that calls `merge.Settings()`. Rename the generate-internal helper to `mergeProfileSettings()` so `mergeSettings()` is available for the former `merge.Settings()`.

**Test helper collisions:**

| Before | After |
|---|---|
| `config_test.go: testConfig()` | `testConfigSetup()` |
| `profile_test.go: testConfig()` | `testProfileConfig()` |
| `generate_test.go: setup()` | `setupGenerateTest()` |

Names that are already unique (`Config`, `ConfigFile`, `importFrom`, `readMeta`, `writeMeta`, `shellQuote`, `isStdoutTTY`) keep their current names.

## What Changes

- All `package cmd` and `package internal/*` declarations become `package main`
- All cross-package imports (`internal/config`, `internal/profile`, etc.) are removed
- Function calls drop package qualifiers: `profile.Create(...)` becomes `createProfile(...)`
- `cmd/cmd_test.go` + `cmd/shell_integration_test.go` merge into `cli_commands_test.go`
- `go.mod` stays the same (module path unchanged, internal imports removed)

## Go-Specific Notes

- **Test packages must use `package main`**, not `package main_test`. Existing tests access unexported symbols (e.g., `rootCmd`, `createFrom`, `shellQuote`), which requires same-package tests.
- **Multiple `init()` functions are legal in Go.** Each command file currently has its own `init()` for `rootCmd.AddCommand(...)`. After consolidation into `cli_commands.go`, these become a single `init()` or are moved into the function body.
- **`getProjectRoot()` in shell_integration_test.go** currently does `filepath.Dir(wd)` to go up from `cmd/` to the project root. After flattening, tests run from the project root, so this must change to just `return wd`.

## What Does NOT Change

- All behavior, output, flags, exit codes
- Test coverage and assertions
- Makefile targets
- `go.mod` module path
- `main.go` remains the entry point

## Verification

1. `make test` passes (verify test count: `go test -v ./... 2>&1 | grep -c '^--- PASS'` before and after)
2. `make build` produces working binary
3. `go vet` clean
4. Smoke test: `apm create work --from current --default` works end-to-end
5. Verify `getProjectRoot` fix: shell integration tests pass
