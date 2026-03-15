# Agent Profile Manager (APM)

A CLI tool for managing multiple Claude Code configuration profiles. Switch between different settings, skills, commands, agents, and rules — per-shell or globally — without restarting Claude Code.

## Why?

Claude Code reads its configuration from `~/.claude/`. If you use Claude for both personal projects and work, you likely want different settings for each: different models, permissions, plugins, environment variables, and custom skills. APM lets you define these as named profiles and switch between them instantly.

## How It Works

APM uses a **common + profile layering** model:

- **Common** settings are shared across all profiles (your baseline configuration)
- **Profile** settings override or extend common settings (per-context customization)
- A **generated** directory is produced by deep-merging common + profile, then symlinking `~/.claude` to it

```
~/.config/apm/
├── common/              ← shared across all profiles
│   ├── settings.json
│   ├── skills/
│   ├── commands/
│   ├── agents/
│   └── rules/
├── profiles/
│   ├── work/            ← profile-specific overrides
│   │   ├── settings.json
│   │   ├── skills/
│   │   └── ...
│   └── personal/
├── generated/           ← merged output (common + profile)
│   ├── work/
│   └── personal/
└── config.yaml          ← tracks active profile, defaults
```

When you activate a profile, `~/.claude` becomes a symlink pointing to the generated directory. Claude Code reads from it transparently.

## Quick Start

### Install

```bash
# Build from source
go build -o bin/apm .

# Install to GOPATH
make install
# or
just install
```

### Create your first profile

Import your current Claude Code settings:

```bash
apm create --current
```

This creates a profile named "default", imports your existing `~/.claude/settings.json` and managed directories, backs up `~/.claude`, and activates the profile. The auto-activation happens because the profile is named "default" — any profile named "default" is automatically set as the global default and activated.

### Create additional profiles

```bash
apm create work --description "Work environment"
apm create personal --from work  # copy from existing profile
```

### Switch profiles

```bash
# Activate in current shell (recommended)
eval "$(apm use work)"

# Set as global default for all new shells
eval "$(apm use work --global)"

# Deactivate (restore original ~/.claude)
eval "$(apm use --unset)"
```

### Shell integration

Add to your `~/.bashrc` or `~/.zshrc` for automatic profile activation and seamless switching:

```bash
eval "$(apm init bash)"   # or: eval "$(apm init zsh)"
```

This provides:
- A shell wrapper that makes `apm use` work without the `eval "$(...)"` prefix
- Sets `APM_PROFILE` env var on new shells if a global default is configured (the symlink must already exist from a prior `apm use` call)

## Commands

| Command | Aliases | Description |
|---------|---------|-------------|
| `apm create [name]` | | Create a profile (defaults to "default") |
| `apm use <profile>` | | Switch to a profile |
| `apm use --unset` | | Deactivate and restore original `~/.claude` |
| `apm ls` | `list` | List all profiles |
| `apm describe <name>` | | Show profile details and settings |
| `apm edit <name>` | | Edit profile's settings.json in `$EDITOR` |
| `apm delete <name>` | `rm` | Delete a profile |
| `apm regenerate [name]` | `regen` | Rebuild a profile's generated directory |
| `apm backup` | | Snapshot current `~/.claude` to backup |
| `apm current` | | Output active profile name |
| `apm init <bash\|zsh>` | | Output shell integration script |

**Global flags**: `--debug` (verbose logging to stderr), `--config-dir <path>` (override config directory)

### `apm create`

```bash
apm create                              # creates "default", auto-activates
apm create work                         # creates empty "work" profile
apm create work --current               # imports from current ~/.claude
apm create work --from personal         # copies from existing profile
apm create work --description "My work" # with description
```

If a profile named "default" is created, it is automatically set as the global default and activated.

### `apm use`

```bash
eval "$(apm use work)"            # activate in this shell
eval "$(apm use work --global)"   # also set as default for new shells
eval "$(apm use --unset)"         # deactivate, restore ~/.claude
```

When stdout is a pipe (the `eval "$(…)"` pattern), `apm use` outputs shell export statements. When stdout is a terminal, it prints human-readable instructions.

### `apm edit`

Opens the profile's `settings.json` in your editor (checks `$VISUAL`, then `$EDITOR`, falls back to `vi`). Validates JSON after editing and automatically regenerates the profile if it is active or is the global default.

### `apm regenerate`

```bash
apm regenerate work     # rebuild one profile
apm regenerate --all    # rebuild all profiles
```

Regeneration preserves runtime state (history, sessions, cache) created by Claude Code. Only managed items (settings, managed directories, symlinks) are rebuilt.

## Settings Merge Rules

When generating a profile, `common/settings.json` is deep-merged with `profiles/<name>/settings.json`:

| Scenario | Behavior |
|----------|----------|
| Scalar values | Profile wins |
| Nested objects | Recursive deep merge |
| `permissions.allow` arrays | **Union** (deduplicated, common items first) |
| `enabledPlugins` objects | **Object merge** (all keys kept, profile wins on conflict) |
| Profile sets value to `null` | **Deletes** the key from the result |
| Type mismatch | Profile wins |

### Example

**common/settings.json:**
```json
{
  "effortLevel": "high",
  "permissions": {
    "allow": ["Read", "Write", "Edit"]
  },
  "enabledPlugins": {
    "plugin-a@marketplace": true
  }
}
```

**profiles/work/settings.json:**
```json
{
  "model": "us.anthropic.claude-opus-4-6-v1[1m]",
  "permissions": {
    "allow": ["Grep", "WebFetch"]
  },
  "enabledPlugins": {
    "gopls-lsp@marketplace": true
  },
  "voiceEnabled": null
}
```

**Result (generated/work/settings.json):**
```json
{
  "effortLevel": "high",
  "model": "us.anthropic.claude-opus-4-6-v1[1m]",
  "permissions": {
    "allow": ["Read", "Write", "Edit", "Grep", "WebFetch"]
  },
  "enabledPlugins": {
    "plugin-a@marketplace": true,
    "gopls-lsp@marketplace": true
  }
}
```

Note: `voiceEnabled` was deleted by the null sentinel, `permissions.allow` was unioned, and `enabledPlugins` was object-merged.

## Managed Directories

Each profile can contain these directories. Files from common and profile are merged via symlinks in the generated directory, with profile entries taking precedence:

- `skills/` — Claude Code skill definitions
- `commands/` — Custom slash commands
- `agents/` — Agent configurations
- `rules/` — Rule files

Extra files (anything not in the managed set, like `CLAUDE.md`) from common and profile are also symlinked into the generated directory, with profile files winning on name conflicts.

## External State Isolation

APM isolates `~/.claude.json` (which stores OAuth tokens and auth state) per profile. When switching profiles, the current profile's `~/.claude.json` is captured and the new profile's is restored. This enables using different Claude accounts across profiles.

When switching to a profile for the first time (no saved external state), `~/.claude.json` is removed to prompt fresh authentication. Your original auth is preserved in the backup and restored when you deactivate all profiles.

## Profile Metadata

Each profile directory contains a `profile.yaml` with metadata:
- `name` — the profile name
- `description` — optional description
- `created_at` — RFC 3339 timestamp
- `source` — how the profile was created (`""`, `"current"`, or source profile name)

## Profile Names

- Lowercase alphanumeric characters and hyphens only
- Must start with a letter or number
- Maximum 50 characters
- Reserved names: `common`, `generated`, `config`, `current`, `state`, `claude-home`, `claude-home-external`, `external`

## Configuration

APM stores its data in `~/.config/apm/` (or `$XDG_CONFIG_HOME/apm/`).

**config.yaml** fields:
- `default_profile` — global default profile for new shells
- `active_profile` — currently symlink-activated profile
- `claude_dir` — override for `~/.claude` location
- `claude_home` — path where the original `~/.claude` was backed up

Override the config directory: `apm --config-dir /path/to/dir <command>`

## Development

```bash
# Build
just build       # or: make build

# Test
just test        # or: make test

# Dev mode (sandboxed, won't touch real ~/.claude)
just dev         # interactive shell with sandboxed config
just run ls      # one-off command in dev mode

# Lint
just vet         # or: make vet
```

Dev mode builds with the `dev` build tag, which auto-sandboxes the config directory to a deterministic path (`/tmp/apm-dev-<uid>`) and uses environment variables (`CLAUDE_CONFIG_DIR`) instead of symlinks.

## Requirements

- Go 1.25+
- bash or zsh (for shell integration)
- macOS or Linux
