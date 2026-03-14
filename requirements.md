# Agent Profile Manager (apm)

## Problem

If you use Claude Code for work and personal projects, you probably have different settings for each: different API backends, models, permissions, plugins, skills. Right now, switching means manually renaming `settings.json` files and swapping directories. It's tedious and easy to mess up.

## Who this is for

People who use Claude Code across multiple contexts and are tired of manually juggling config files. Typical case: Bedrock via AWS SSO at work, direct Anthropic API for personal stuff, different plugins and skills for each.

## Goal

Single-command profile switching for Claude Code. A profile can inherit shared settings from a common base, override what it needs to, or ignore the base entirely.

## Definitions

### What is a "profile"?

A profile manages:
- `settings.json` (permissions, env vars, model, plugins, statusline config)
- `skills/` (installed skills)
- `commands/` (slash commands)
- `agents/` (custom agent definitions)

Everything else in `~/.claude/` is shared across profiles and left alone:
- `settings.local.json` (machine-specific overrides, always applied)
- `history.jsonl`, `projects/`, `sessions/`, `plugins/`
- `cache/`, `debug/`, `backups/`, `file-history/`, `shell-snapshots/`, `paste-cache/`, `session-env/`, `stats-cache.json`, `cost_history.json`, etc.

### How switching works

Claude Code reads `CLAUDE_CONFIG_DIR` (defaults to `~/.claude`). APM builds a generated directory per profile, each one a valid `CLAUDE_CONFIG_DIR` target with merged settings and symlinks back to shared items in `~/.claude/`. The original `~/.claude/` is never touched.

### Layering

Two layers:
1. Common base (`~/.config/apm/common/`) -- shared settings, skills, commands, agents
2. Profile overrides (`~/.config/apm/profiles/<name>/`) -- profile-specific settings

These get deep-merged into a generated directory at switch time:
- Objects: recursive merge, profile keys win, common-only keys kept
- Scalars: profile overrides common
- Arrays: profile replaces common entirely
- `permissions.allow`: union (deduplicated). Silently dropping a permission grant would be a nasty bug.
- `enabledPlugins`: object merge, both sides kept, profile wins on conflict
- Null sentinel: setting a key to `null` in the profile removes it from merged output

For directories (skills/, commands/, agents/): contents from both layers are symlinked into the generated directory. Same-name conflicts go to the profile version.

## Requirements

### CLI

Go CLI using `cobra`. Subcommands with minimal flags.

Global flags: `--debug` (verbose logging to stderr), `--config-dir PATH` (override config location).

### Switching

- `apm use <profile>` -- per-shell. Sets `CLAUDE_CONFIG_DIR` in the current shell.
- `apm use <profile> --global` -- sets the default for new shells.
- `apm use --unset` -- deactivate in current shell.
- Per-shell switching needs shell integration: `eval "$(apm init bash)"` in your rc file. This wraps `apm use` to auto-eval its output and activates the global default on shell startup.

### Profile management

- `apm create <name> [--from current|<profile>] [--description TEXT]`
  - No `--from`: empty profile, inherits only from common base
  - `--from current`: imports from current `~/.claude/`
  - `--from <profile>`: copies from an existing profile
- `apm delete <name> [--force]` -- refuses if active unless `--force`
- `apm edit <name>` -- opens profile's `settings.json` in `$EDITOR`, validates JSON on save, regenerates
- `apm ls` -- lists profiles, marks which is active (global default vs current shell)
- `apm describe <name>` -- shows metadata, overrides, managed artifacts

### Status query

`apm current` outputs the active profile name to stdout. Nothing else. Meant for statusline scripts:

```bash
profile=$(apm current 2>/dev/null)
```

Resolution: `$APM_PROFILE` env var > `config.yaml` default > exit 1.

### Shell integration

`apm init <bash|zsh>` outputs a shell function that wraps `apm use` and auto-activates the global default on startup.

### Regeneration

`apm regenerate [<name>|--all]` rebuilds generated directories. `apm use` always regenerates to catch manual edits.

### Config location

Defaults to `~/.config/apm/` (or `$XDG_CONFIG_HOME/apm/`). Override with `--config-dir`.

## Non-goals

- Other CLI tools (AWS, kubectl, etc.). Only Claude Code.
- Interactive/TUI mode. All operations are single commands.
- Modifying `~/.claude/` directly. APM is non-destructive.
- Per-profile `settings.local.json`. It stays machine-specific.
- Profile versioning or migration. Out of scope for v1.

## UX

- Minimal flags. Profile name, debug, config dir. That's it.
- Errors tell you what to do: "Profile 'work' is active. Use --force to delete anyway."
- Running `apm use` without `eval` detects it and prints instructions.
- `apm current` is fast and silent on failure.

## Tech stack

- Go >=1.22
- cobra (CLI), lipgloss (colors/styling)
- go build (single binary), go install (distribution)
- go test (testing)
