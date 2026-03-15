# AGENTS.md — Quick Context for AI Agents

## What is this project?

Agent Profile Manager (APM) — a Go CLI tool (`apm`) that manages multiple Claude Code
configuration profiles. Users switch between different settings, skills, commands, agents,
and rules by generating merged profile directories and symlinking `~/.claude` to the active one.

## Key concepts

- **Common** (`~/.config/apm/common/`) — shared base settings for all profiles
- **Profile** (`~/.config/apm/profiles/<name>/`) — per-profile overrides
- **Generated** (`~/.config/apm/generated/<name>/`) — merged output (common + profile), what Claude reads
- **Activation** — `~/.claude` becomes a symlink to the generated dir
- **External state** — `~/.claude.json` (OAuth tokens) is isolated per-profile

## Project structure

Single Go package (`main`), flat structure. Every `.go` file is in the root directory.

| File | Responsibility |
|------|---------------|
| `main.go` | Entry point, runs Cobra root command |
| `constants.go` | Managed dirs/items, reserved names, path helpers |
| `config.go` | `Config` struct, `config.yaml` I/O, directory setup |
| `profile.go` | Profile CRUD: create, delete, list, import |
| `activate.go` | Symlink activation/deactivation, backup, external state |
| `generate.go` | Profile generation: merge settings + symlink dirs |
| `merge.go` | JSON deep merge engine (union arrays, null deletion, object merge) |
| `shell.go` | Shell integration script generation (bash/zsh) |
| `validate.go` | Profile name and settings.json validation |
| `cli_commands.go` | All Cobra CLI commands and flags |
| `dev.go` | Dev-mode sandbox (build tag: `dev`) |

## Important files to read first

1. **`cli_commands.go`** — all user-facing commands and the CLI flow
2. **`generate.go`** — the generation pipeline (core of the tool)
3. **`activate.go`** — symlink management and external state isolation
4. **`merge.go`** — settings deep merge logic with special-case strategies

## Build and test

```bash
go build -o bin/apm .         # build
go test -count=1 -race ./...  # test
go build -tags dev -o bin/apm-dev .  # dev build (sandboxed)
```

Or use `just build`, `just test`, `just dev`.

## Key design rules

1. **Generated = common + profile only.** Backup (`claude-home/`) never leaks in.
2. **Runtime state survives regeneration.** Only managed items are cleaned.
3. **Profile wins on conflict.** In every merge (settings, dirs, extras).
4. **Null sentinel deletes keys.** Setting a value to `null` in profile removes it from output.
5. **Symlink mode vs dev mode.** Normal: `~/.claude` symlink. Dev: `CLAUDE_CONFIG_DIR` env var.

## Managed items (what APM controls)

`settings.json`, `.apm-meta.json`, `skills/`, `commands/`, `agents/`, `rules/`

Everything else in a generated dir is runtime state (history, sessions, cache) and is preserved.

## Settings merge strategies

| Path | Strategy |
|------|----------|
| `permissions.allow` | Union arrays (deduplicated) |
| `enabledPlugins` | Object merge (merge keys, profile wins) |
| Nested objects | Recursive deep merge |
| `null` values | Delete from result |
| Everything else | Profile replaces common |

## Documentation

- `README.md` — user guide, quick start, all commands, merge rules
- `docs/architecture.md` — detailed internal architecture, module-by-module walkthrough, control flows
