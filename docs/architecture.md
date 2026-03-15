# Architecture

This document describes the internal architecture of APM: how the code is organized, how modules connect, and how key operations work from end to end.

## Module Map

APM is a single Go package (`main`). Each source file has a focused responsibility:

```
main.go            Entry point — runs Cobra root command
constants.go       Path helpers, managed item definitions, reserved names
config.go          Config struct, config.yaml I/O, directory management
profile.go         Profile CRUD: create, delete, list, import, metadata
activate.go        Symlink activation/deactivation, backup, external state
generate.go        Profile generation pipeline: merge + symlink
merge.go           JSON deep merge engine with special-case strategies
shell.go           Shell integration script generation (bash/zsh)
validate.go        Profile name and settings.json validation
cli_commands.go    All Cobra commands and their flags
dev.go             Dev-mode sandbox (build tag: dev)
testutil_test.go   Shared test helper (newTestConfig)
```

## Data Flow Overview

```
User edits:                     APM generates:                Claude Code reads:

common/settings.json ──┐
common/skills/      ──┐│     ┌──────────────┐
common/commands/    ──┤├────▶│ generateProfile│──▶ generated/<name>/
common/agents/      ──┤│     └──────────────┘     ├── settings.json (merged)
common/rules/       ──┘│                           ├── skills/  (symlinks)
                       │                           ├── commands/ (symlinks)
profiles/<name>/       │                           ├── agents/  (symlinks)
  settings.json  ──────┘                           ├── rules/   (symlinks)
  skills/        ──────────────────────────────────▶├── [extras] (symlinks)
  commands/      ──────────────────────────────────▶└── .apm-meta.json
  agents/        ──────────────────────────────────▶
  rules/         ──────────────────────────────────▶

                    ~/.claude ──symlink──▶ generated/<name>/
```

## Source File Details

### main.go

The entry point. Runs `rootCmd.Execute()` (Cobra). Has special handling for the `errNoActiveProfile` sentinel: when `apm current` finds no active profile, it exits with code 1 silently (no error message printed to stderr). This enables statusline scripts to call `apm current` without noisy error output.

```go
func main() {
    if err := rootCmd.Execute(); err != nil {
        if isNoActiveProfile(err) {
            os.Exit(1)  // silent exit for statusline scripts
        }
        fmt.Fprintf(os.Stderr, "error: %s\n", err)
        os.Exit(1)
    }
}
```

### constants.go

Defines the vocabulary of "what APM manages" and provides path resolution.

**Managed items** — the set of files and directories that APM controls via generation:

| Item | Type | Description |
|------|------|-------------|
| `settings.json` | File | Merged from common + profile |
| `.apm-meta.json` | File | Generation metadata (settings hash) |
| `skills/` | Dir | Skill definitions |
| `commands/` | Dir | Custom slash commands |
| `agents/` | Dir | Agent configurations |
| `rules/` | Dir | Rule files |

Anything NOT in this set is considered **runtime state** (created by Claude Code during operation) and is preserved across regenerations.

**Reserved profile names**: `common`, `generated`, `config`, `current`, `state`, `claude-home`, `claude-home-external`, `external`

**Path helpers**:
- `defaultClaudeDir()` → `~/.claude` — intentionally ignores `CLAUDE_CONFIG_DIR` env var because that points to a generated dir when a profile is active
- `claudeJSONPath()` → `~/.claude.json` — external auth state location
- `defaultAPMDir()` → `~/.config/apm` (or `$XDG_CONFIG_HOME/apm`)

### config.go

Manages the `Config` struct (runtime paths) and `ConfigFile` (persisted YAML).

**ConfigFile** (persisted in `config.yaml`):

| Field | YAML Key | Description |
|-------|----------|-------------|
| `DefaultProfile` | `default_profile` | Global default for new shells |
| `ClaudeDir` | `claude_dir` | User override for `~/.claude` location |
| `ActiveProfile` | `active_profile` | Currently symlink-activated profile |
| `ClaudeHomePath` | `claude_home` | Where original `~/.claude` was backed up |

**Config** (runtime, resolved from config.yaml):

| Field | Typical Value | Description |
|-------|---------------|-------------|
| `APMDir` | `~/.config/apm` | Root config directory |
| `ClaudeDir` | `~/.claude` or backup path | Where Claude Code's real config lives |
| `CommonDir` | `APMDir/common` | Shared settings/assets |
| `ProfilesDir` | `APMDir/profiles` | Per-profile source directories |
| `GeneratedDir` | `APMDir/generated` | Generated output directories |
| `ConfigPath` | `APMDir/config.yaml` | Config file path |

**`loadConfig(apmDirOverride)`** — resolution priority for `ClaudeDir`:
1. `config.yaml` → `claude_dir` (explicit user override)
2. `config.yaml` → `claude_home` (backup path set during first activation)
3. `~/.claude` (default)

**`EnsureDirs()`** — creates the full directory structure:
- `common/` with subdirs for each managed dir + seeds `settings.json` with `{}`
- `profiles/`, `generated/`
- `.gitignore` excluding `*/external/` and `claude-home-external/` (OAuth tokens)

**`atomicWriteFile()`** — writes via temp file + `os.Rename` for crash safety. Used for config.yaml and generated settings.json.

**`ClearActiveProfile()`** — clears both `active_profile` AND `claude_home` from config.yaml. This is important during deactivation because it resets the backup path tracking.

### profile.go

Profile CRUD operations and metadata management.

**`createProfile(cfg, name, source, description)`** — three source modes:

| Source | Behavior |
|--------|----------|
| `""` or `"empty"` | Empty profile with `{}` settings.json |
| `"current"` | Import from `~/.claude` (copies settings.json + managed dirs via `importFrom()`) |
| `"<name>"` | Copy from another existing profile |

After importing, validates that `settings.json` is a valid JSON object. On any failure, the partially-created profile directory is cleaned up via `os.RemoveAll()`.

**`deleteProfile(cfg, name, force)`** — safety checks before deletion:
- Without `--force`: refuses if profile is the global default (`config.yaml default_profile`) or active in the current shell (`APM_PROFILE` env var)
- Clears `default_profile` in config.yaml if this was the default
- Removes both the generated dir and profile dir

Note: the check for "active in current shell" uses the `APM_PROFILE` environment variable, NOT `active_profile` from config.yaml. The symlink-level deactivation (when the active profile is deleted) is handled by `deleteCmd` in cli_commands.go, not by `deleteProfile()` itself.

**`importFrom(src, dst)`** — copies `settings.json` + all managed dirs from source to destination. If source has no `settings.json`, writes `{}`. Uses `copyDir()` which preserves symlinks.

**`seedRuntimeState(claudeDir, genDir)`** — copies non-managed items (history, sessions, plugins, etc.) from `claudeDir` to `genDir`. Skips managed items (`managedItemSet`) and `profile.yaml`. Preserves symlinks. Used when creating a profile from `--current` to carry over Claude Code's runtime state.

**`profileExists()`** — returns `false` for both missing profiles AND unexpected stat errors (logging the latter). Callers that need to distinguish these cases should use `getProfile()` instead.

### activate.go

The core activation/deactivation machinery. This is the most complex module — it manages the `~/.claude` symlink and per-profile external state (`~/.claude.json`).

**`activateProfile(cfg, name, skipSymlink)`** handles four distinct states of `~/.claude`:

| State | Action |
|-------|--------|
| `skipSymlink=true` (dev mode) | Returns `errSkipSymlink` immediately |
| Symlink exists (switching profiles) | Capture old profile's external state, remove symlink |
| Real directory exists (first activation) | Move to backup, capture external state backup |
| Real directory + backup already exists | Error: inconsistent state, resolve manually |
| Does not exist | Create empty backup dir |

After state handling:
1. Calls `generateProfile()` to build the merged output
2. Creates symlink: `~/.claude` → `generated/<name>`
3. Restores new profile's external state (`~/.claude.json`)
4. Records `active_profile` in config.yaml

Cross-filesystem moves (e.g., `~/.claude` on one mount, APM config on another) are detected via `syscall.EXDEV` with a helpful error message.

**`deactivateProfile(cfg)`**:
1. Captures active profile's external state
2. Removes `~/.claude` symlink
3. Restores `~/.claude` from backup (if `claude_home` is set in config)
4. Restores backup's external state (`~/.claude.json`)
5. Clears `active_profile` and `claude_home` from config.yaml

**`backupClaude(cfg)`** — manual backup command. Refuses if a profile is active (because `~/.claude` would be a symlink, not the real config). Copies `~/.claude` directory tree + captures `~/.claude.json`. Also writes `claude_home` to config.yaml so that `loadConfig()` resolves `ClaudeDir` to the backup path on subsequent runs.

**External state isolation** — `captureExternalState()` / `restoreExternalState()` / `restoreExternalStateTo()`:
- `~/.claude.json` stores OAuth tokens and auth state
- Each profile has a `profiles/<name>/external/claude.json` for its auth state
- On profile switch: old profile's state is captured, new profile's state is restored
- On first use of a new profile: if no saved state exists, `~/.claude.json` is **removed** to prompt fresh authentication
- Backup external state stored in `claude-home-external/claude.json`
- `restoreExternalStateTo(src, dst)` copies external state between arbitrary directories (used during profile overwrite)

**`copyDir(src, dst)`** — recursive copy using `filepath.WalkDir`. Preserves symlinks (copies the link, not the target). Used by backup, import, and seed operations.

### generate.go

Builds the generated directory by merging common + profile sources. This is the **only** place where the generated dir content is determined — backup/claude-home never leaks into generated directories.

**`generateProfile(cfg, name)`** — 5-step pipeline:

1. **Validate** — check both `common/settings.json` and `profiles/<name>/settings.json` are valid JSON objects
2. **Merge settings** — deep merge `common/settings.json` + `profiles/<name>/settings.json` → write to `generated/<name>/settings.json`
3. **Merge managed dirs** — for each dir (`skills/`, `commands/`, `agents/`, `rules/`): create dir in generated, symlink profile entries first (they win), then common entries (skip on name conflict)
4. **Symlink extras** — link non-managed files from profile (wins) then common into generated dir. Skips managed items and `profile.yaml`
5. **Write meta hash** — `.apm-meta.json` with `{"settings_hash": "<sha256-hex>"}` for staleness detection

**`cleanManagedItems(genDir)`** — called before regeneration:
- Removes items in `managedItemSet` (settings.json, .apm-meta.json, managed dirs)
- Removes root-level symlinks only (extras from common/profile)
- Preserves regular files and directories (runtime state from Claude Code)

This design means regeneration is safe to run while Claude Code is using the profile — only managed items are rebuilt, runtime files (history.jsonl, sessions/, cache/, etc.) survive.

**Symlink resolution** — both `symlinkEntries()` and `linkExtrasFrom()` call `filepath.EvalSymlinks(src)` before creating the output symlink. If a file in `common/skills/` is itself a symlink, the generated dir will symlink to the resolved real path, not the intermediate symlink.

### merge.go

The JSON deep merge engine. Produces a new map without mutating inputs.

**`mergeSettings(common, profile)`** — three phases:
1. `deepMerge(result, common, "")` — copy common into result
2. `deepMerge(result, profile, "")` — apply profile overrides
3. `applyDeletions(result, profile, "")` — remove keys set to `null`

**Merge strategies by JSON path:**

| Strategy | Paths | Behavior |
|----------|-------|----------|
| Union array | `permissions.allow` | Deduplicated merge, common items first, then new profile items |
| Object merge | `enabledPlugins` | Merge all keys from both, profile values win on conflict |
| Recursive merge | Any nested object | Merge sub-keys recursively |
| Null deletion | Any key set to `null` | Remove from result (even works nested) |
| Default | Everything else | Profile value replaces common value |

**Important asymmetry**: `unionArrayKeys` matches on the full dotted path (e.g., must be exactly `permissions.allow`). `objectMergeKeys` matches on both the full path AND the bare key name — so `enabledPlugins` would be object-merged even if found nested inside another object.

**Aliasing prevention** — `cloneValue()` / `cloneMap()` deep-copy values in the default/fallback merge case (`dst[key] = cloneValue(srcVal)`) to prevent common's nested maps from being aliased into the result and then mutated by subsequent profile merge passes. Recursive object merges (`deepMerge(dstMap, srcMap, path)`) operate on the result map directly. The cloning was added to fix a bug where nested maps from common ended up in the result by reference, and the profile merge pass mutated the originals.

**`loadJSON(path)`** — returns an empty map (not an error) when the file doesn't exist. This means a profile without `settings.json` is treated as having `{}` settings.

### shell.go

Generates shell integration scripts using a Go template with bracket placeholders (`%[1]s` / `%[2]s`) for bash `[ ]` vs zsh `[[ ]]`.

**The shell wrapper `apm()`**:
- Intercepts only the `use` subcommand
- Runs `command apm "$@"` (bypasses the function to call the real binary)
- Captures stdout to a temp file, stderr to another
- If exit code is 0: evals the stdout (which contains `export` statements)
- Always passes stderr through
- All other subcommands pass through directly

**`_apm_auto_activate()`** — runs on shell startup:
- If `APM_PROFILE` is not set, calls `command apm current` to read the active/default profile name
- If a profile name is returned, exports `APM_PROFILE` in the environment
- This is for **env-var/display purposes only** — it does NOT call `apm use`, does not set `CLAUDE_CONFIG_DIR`, and does not create or modify the `~/.claude` symlink. The symlink must already exist from a prior explicit `apm use` call for Claude Code to actually read the right config. New shells simply learn the active profile name.

### validate.go

**`validateProfileName(name)`** — enforces:
- Non-empty, max 50 characters
- Matches `^[a-z0-9][a-z0-9-]*$` (lowercase alphanumeric + hyphens, no leading hyphen)
- Not in `reservedNames` set

**`validateSettingsJSON(path)`** — loads JSON from file:
- Must parse as a JSON object (not array, string, number, or null)
- Missing file is treated as valid (empty `{}`)
- Invalid JSON returns an error

### cli_commands.go

All Cobra commands and the CLI framework setup.

**Global flags**:
- `--debug` — enables verbose logging to stderr
- `--config-dir` — overrides APM config directory (default: `~/.config/apm`)

**`createCmd`** — `apm create [name]`:
- Name defaults to `"default"` if omitted
- Flags: `--current` (import from `~/.claude`), `--from <profile>` (copy), `--description`
- If profile exists: prompts for overwrite confirmation; if confirmed, deactivates (if active), deletes, recreates, preserves external state via temp dir
- If name is `"default"`: auto-sets as global default and activates
- Seeds runtime state and captures external state for `--current` profiles

**`useCmd`** — `apm use <profile>`:
- Flag: `--global` (set as default for new shells), `--unset` (deactivate)
- TTY-aware output: human messages when stdout is a terminal, shell export statements when piped
- Normal mode: creates symlink via `activateProfile()`, stdout emits `export APM_PROFILE='<name>'` only
- Dev mode (`errSkipSymlink`): generates profile, stdout emits both `APM_PROFILE` and `CLAUDE_CONFIG_DIR` exports
- `--unset`: deactivates symlink (normal mode) or emits `unset` commands (dev mode)

**`deleteCmd`** — `apm delete <name>` (alias: `rm`):
- Flag: `--force` (delete even if active/default)
- After deletion: auto-deactivates if the deleted profile was the active one OR if no profiles remain
- Clears global default if the deleted profile was it
- Warns if `APM_PROFILE` env var still references the deleted profile

**`editCmd`** — `apm edit <name>`:
- Opens `profiles/<name>/settings.json` in editor (`$VISUAL` > `$EDITOR` > `vi`)
- Uses `sh -c editor "$@"` for safe parsing of editors with flags/spaces
- Validates JSON after edit; warns but doesn't error on invalid JSON
- Auto-regenerates if profile is active in current shell (`APM_PROFILE` env) OR is the global default

**`lsCmd`** — `apm ls` (alias: `list`):
- Shows all profiles with markers: `*` for active (shell env or config) or global default
- Shows description if set

**`currentCmd`** — `apm current`:
- Priority: `APM_PROFILE` env → `config.yaml active_profile` → `config.yaml default_profile`
- Returns `errNoActiveProfile` (custom type, detected via `errors.As`) for silent exit code 1

**`initCmd`** — `apm init <bash|zsh>`: outputs the shell integration script

**`regenerateCmd`** — `apm regenerate [name]` (alias: `regen`):
- Flag: `--all` (regenerate all profiles)
- When regenerating all: active profile is regenerated last with a warning

**`backupCmd`** — `apm backup`: snapshots current `~/.claude` to backup location

### dev.go (build tag: dev)

Only compiled with `-tags dev`. Provides safe development:
- `devConfigDir()` — returns `/tmp/apm-dev-<uid>` as a stable sandbox directory
- Overrides `PersistentPreRun` to auto-set `--config-dir` to the sandbox
- Uses `cmd.Flags().Set("config-dir", dir)` so `Changed()` returns true — this triggers env-var mode in `useCmd` (skips symlink activation), preventing dev builds from touching the real `~/.claude`
- The sandbox path is deterministic (`/tmp/apm-dev-<uid>`), not ephemeral — it persists across dev invocations

## Key Invariants

1. **Generated = common + profile only.** The backup (`claude-home/`) NEVER leaks into generated directories.
2. **Runtime state survives regeneration.** `cleanManagedItems()` only removes managed items and root-level symlinks.
3. **Atomic config writes.** `config.yaml` and generated `settings.json` are written via temp file + rename (`atomicWriteFile`). Note: `.apm-meta.json` uses `os.WriteFile` directly as it is non-critical metadata.
4. **Profile wins on conflict.** In every merge operation (settings, dirs, extras), profile content takes precedence over common.
5. **External state is isolated per-profile.** Each profile can have its own `~/.claude.json` (OAuth tokens).
6. **Dev mode never touches `~/.claude`.** Uses env vars instead of symlinks.
