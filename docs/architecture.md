# Architecture

This document describes the internal architecture of APM: how the code is organized, how modules connect, and how key operations work from end to end.

## Module Map

APM is a single Go package (`main`). Each source file has a focused responsibility:

```
main.go            Entry point — runs Cobra root command
cli.go             All Cobra commands, flags, color/TTY helpers, shell integration scripts
config.go          Config struct, config.yaml I/O, directory management
statusline.go      Default statusline script embedding and settings patching
profile.go         All business logic: CRUD, activation/nuke, generation, merge, env, hashing, validation
helpers.go         Shared constants, version info, path helpers, atomicWriteFile, isSymlink, shellQuote
dev.go             Dev-mode sandbox (build tag: dev)
testutil_test.go   Shared test helper (newTestConfig)
```

## Data Flow Overview

```
User edits:                     APM generates:                Claude Code reads:

common/settings.json ──┐
common/env           ──┐│     ┌──────────────┐
common/skills/      ──┐││     │ generateProfile│──▶ generated/<name>/
common/commands/    ──┤├┤────▶│              │     ├── settings.json (merged)
common/agents/      ──┤││     └──────────────┘     ├── env          (merged)
common/rules/       ──┘││                           ├── skills/  (symlinks)
                       ││                           ├── commands/ (symlinks)
profiles/<name>/       ││                           ├── agents/  (symlinks)
  settings.json  ──────┘│                           ├── rules/   (symlinks)
  env            ────────┘                          ├── [extras] (symlinks)
  skills/        ──────────────────────────────────▶├── .claude.json (auth)
  commands/      ──────────────────────────────────▶└── .apm-meta.json
  agents/        ──────────────────────────────────▶
  rules/         ──────────────────────────────────▶

             CLAUDE_CONFIG_DIR ──▶ generated/<name>/
```

## Source File Details

### main.go

The entry point. Runs `rootCmd.Execute()` (Cobra). Has special handling for the `errNoActiveProfile` sentinel: when `apm current` finds no active profile, it exits with code 1 silently (no error message printed to stderr). This enables statusline scripts to call `apm current` without noisy error output.

### cli.go

All Cobra commands, CLI framework setup, color/TTY helpers, and shell integration scripts.

**Color/TTY helpers**: ANSI color variables (`colorGreen`, etc.), `shouldDisableColor()`, `initColor()`, `isStdoutTTY()` for TTY-aware output.

**Shell integration**: Generates shell scripts for bash and zsh.

- **The shell wrapper `apm()`**: intercepts `use` and `nuke` subcommands, runs the real binary, captures stdout/stderr separately. If exit code 0: evals stdout (export/unset statements). All other subcommands pass through directly.
- **`_apm_auto_activate()`**: runs on shell startup. If `APM_PROFILE` is not set, calls `apm current` to get the active profile and runs `eval "$(apm use "$profile")"` to set `CLAUDE_CONFIG_DIR` and env vars.

**Commands**:

- **`useCmd`** — `apm use <profile>`: TTY-aware (human messages on terminal, shell exports when piped). Non-TTY output: unsets previous profile env vars, then exports `APM_PROFILE`, `CLAUDE_CONFIG_DIR`, plus profile env vars. `--unset`: reads active profile for env cleanup, deactivates, emits unset commands.
- **`createCmd`** — `apm create [name]`: Name defaults to `"default"` if omitted. Calls `generateProfile` directly, then seeds runtime state and copies `.claude.json` for `--current`. Calls `SetActiveProfile` directly (skips `ensureFresh` since profile was just generated).
- **`editCmd`** — `apm edit <name>`: Opens source settings.json (or env with `--env`) in editor. Auto-regenerates if profile is active.
- **`regenerateCmd`** — `apm regenerate [name]` (hidden): No args = regenerate all; optional name = just that profile.
- **`deleteCmd`** — `apm delete <name>` (alias: `rm`): Auto-deactivates if deleted profile was active.
- **`renameCmd`** — `apm rename <old> <new>` (alias: `mv`): Renames profile and generated dirs, regenerates to fix symlink targets.
- **`nukeCmd`** — `apm nuke`: Flattens active generated dir into `~/.claude`, removes APM data.

### config.go

Manages the `Config` struct (runtime paths) and `ConfigFile` (persisted YAML).

**ConfigFile** (persisted in `config.yaml`):

| Field | YAML Key | Description |
|-------|----------|-------------|
| `DefaultProfile` | `default_profile` | Global default for new shells |
| `ClaudeDir` | `claude_dir` | User override for `~/.claude` location |
| `ActiveProfile` | `active_profile` | Currently active profile |

**Config** (runtime, resolved from config.yaml):

| Field | Typical Value | Description |
|-------|---------------|-------------|
| `APMDir` | `~/.config/apm` | Root config directory |
| `ClaudeDir` | `~/.claude` | Where Claude Code's real config lives |
| `CommonDir` | `APMDir/common` | Shared settings/assets |
| `ProfilesDir` | `APMDir/profiles` | Per-profile source directories |
| `GeneratedDir` | `APMDir/generated` | Generated output directories |
| `ConfigPath` | `APMDir/config.yaml` | Config file path |

**`loadConfig(apmDirOverride)`** — resolution priority for `ClaudeDir`:
1. `config.yaml` → `claude_dir` (explicit user override)
2. `~/.claude` (default)

Also calls `migrateFromSymlinks()` for one-time migration from the old symlink model.

**`EnsureDirs()`** — creates the full directory structure:
- `common/` with subdirs for each managed dir + seeds `settings.json` with default statusLine config
- `common/statusline.sh` (default statusline script)
- `profiles/`, `generated/`
- `.gitignore` excluding `generated/*/.claude.json` (OAuth tokens)

### statusline.go

Embeds the default `statusline.sh` script and manages statusline configuration.

**`writeDefaultStatusLine(commonDir, claudeDir)`** — writes `statusline.sh` to `common/` if absent, patches `statusLine` config into both `common/settings.json` and `~/.claude/settings.json` (so statusline works even without apm activation).

**`ensureStatusLineInSettings(path, commonDir)`** — adds `statusLine` key to a settings.json file if not already present.

### profile.go

The largest file — contains all business logic consolidated from the former `profile.go`, `activate.go`, `generate.go`, `input_hash.go`, `merge.go`, `envfile.go`, and `validate.go`. Major subsections:

**Profile CRUD** — `createProfile`, `deleteProfile`, `listProfiles`, `renameProfile`, `importFrom`, `seedRuntimeState`. Three source modes for create: empty, current (`~/.claude`), or copy from another profile. Validates settings.json on import. Cleans up on failure.

**Validation** — `validateProfileName` (length, reserved names, invalid chars), `validateSettingsJSON` (must be a valid JSON object).

**Activation & Nuke** — `activateProfile` (calls `ensureFresh`, records active profile), `deactivateProfile`, `nukeAPM` (flattens active generated dir into `~/.claude`, removes APM data), `flattenGeneratedDir`, `copyClaudeJSON`.

**Migration** — `migrateFromSymlinks` (one-time migration from old symlink model: restores backup, migrates `.claude.json`, cleans up legacy dirs, writes `.migrated-v2` marker).

**Generation pipeline** — `generateProfile` (6-step pipeline: validate, merge settings, merge managed dirs, merge env files, symlink extras, write meta hash), `ensureFresh` (lazy staleness check on every activation), `cleanManagedItems`.

**Settings merge** — `deepMergeSettings` (JSON deep merge engine). Merge strategies by path:

| Strategy | Paths | Behavior |
|----------|-------|----------|
| Union array | `permissions.allow` | Deduplicated merge |
| Object merge | `enabledPlugins` | Merge all keys, profile wins on conflict |
| Recursive merge | Any nested object | Merge sub-keys recursively |
| Null deletion | Any key set to `null` | Remove from result |
| Default | Everything else | Profile replaces common |

**Env file handling** — `parseEnvFile`, `mergeEnvFiles` (common + profile, profile wins), `readEnvExports` / `readEnvUnsets` (shell export/unset statement generation via `envShellLines`).

**Input hashing** — `computeInputHash` (SHA-256 of source file paths + mtimes), `isProfileStale` (compares current vs stored hash), `readStoredInputHash`.

**File operations** — `copyDir` (recursive, preserves symlinks), `copyDirFlat` (recursive, resolves symlinks), `unlinkAll` (removes symlinks before `RemoveAll`).

### helpers.go

Shared constants, version info, and utility functions.

**Managed items** — the set of files and directories that APM controls via generation:

| Item | Type | Description |
|------|------|-------------|
| `settings.json` | File | Merged from common + profile |
| `.apm-meta.json` | File | Generation metadata (input hash for staleness detection) |
| `env` | File | Merged environment variables |
| `skills/` | Dir | Skill definitions |
| `commands/` | Dir | Custom slash commands |
| `agents/` | Dir | Agent configurations |
| `rules/` | Dir | Rule files |

Anything NOT in this set is considered **runtime state** (created by Claude Code during operation) and is preserved across regenerations.

**File name constants**: `envFileName` (`env`), `profileMetaFile` (`profile.yaml`), `apmMetaFile` (`.apm-meta.json`), `metaKeyInputHash` (`input_hash`)

**Reserved profile names**: `common`, `generated`, `config`, `current`, `state`

**Path helpers**:
- `defaultClaudeDir()` → `~/.claude` — intentionally ignores `CLAUDE_CONFIG_DIR` env var because that points to a generated dir when a profile is active
- `defaultAPMDir()` → `~/.config/apm` (or `$XDG_CONFIG_HOME/apm`)

**Utility functions**: `atomicWriteFile` (writes via temp file + `os.Rename` for crash safety), `isSymlink`, `shellQuote`, `versionInfo`.

### dev.go (build tag: dev)

Only compiled with `-tags dev`. Auto-sandboxes config to `/tmp/apm-dev-<uid>`.

## Key Invariants

1. **Generated = common + profile only.** No other sources leak in.
2. **Runtime state survives regeneration.** `cleanManagedItems()` only removes managed items and root-level symlinks.
3. **Atomic config writes.** `config.yaml` and generated `settings.json` use `atomicWriteFile`.
4. **Profile wins on conflict.** In every merge operation.
5. **Env vars are isolated.** Switching profiles unsets old vars before exporting new ones.
6. **Activation is env-var based.** `CLAUDE_CONFIG_DIR` points to the generated dir. No symlinks.
7. **Auto-regeneration on staleness.** Every activation checks input hash and regenerates if sources changed. No manual `regenerate` needed.
