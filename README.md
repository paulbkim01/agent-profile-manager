# Agent Profile Manager (APM)

[English](README.md) | [한국어](README.ko.md)

A CLI tool for managing multiple Claude Code configurations. Save different setups as profiles and swap between them without restarting Claude.

## Why?

Claude Code stores everything in `~/.claude/`. That works fine until you need different models for work, different permissions for a client project, or separate AWS credentials for personal stuff. APM lets you save each setup as a named profile and switch between them.

## How it works

APM layers config in two tiers:

- **Common** — shared baseline across every profile
- **Profile** — overrides for a specific context

When you activate a profile, APM deep-merges common + profile into a generated directory and points `CLAUDE_CONFIG_DIR` at it. Claude Code reads from there without knowing anything changed.

```
~/.config/apm/
├── common/              ← shared baseline
│   ├── settings.json
│   ├── env              ← shared environment variables
│   ├── skills/
│   ├── commands/
│   ├── agents/
│   └── rules/
├── profiles/
│   ├── work/            ← per-profile overrides
│   │   ├── settings.json
│   │   ├── env          ← profile-specific env vars
│   │   ├── skills/
│   │   └── ...
│   └── personal/
├── generated/           ← merged output (common + profile)
│   ├── work/
│   └── personal/
└── config.yaml          ← active profile, defaults
```

## Install

### Script (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/paulbkim01/agent-profile-manager/master/scripts/install.sh | bash
```

Downloads the binary and adds shell integration (`eval "$(apm init ...)"`) to your `~/.bashrc`, `~/.bash_profile`, or `~/.zshrc`.

To install somewhere else:

```bash
APM_INSTALL_DIR=~/.local/bin curl -fsSL https://raw.githubusercontent.com/paulbkim01/agent-profile-manager/master/scripts/install.sh | bash
```

To pin a specific version:

```bash
APM_VERSION=v0.1.0 curl -fsSL https://raw.githubusercontent.com/paulbkim01/agent-profile-manager/master/scripts/install.sh | bash
```

### Go install

```bash
go install github.com/paulbkim01/agent-profile-manager@latest
mv "$(go env GOPATH)/bin/agent-profile-manager" "$(go env GOPATH)/bin/apm"
```

### Build from source

```bash
git clone https://github.com/paulbkim01/agent-profile-manager.git
cd agent-profile-manager
make install   # or: just install
```

### Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/paulbkim01/agent-profile-manager/master/scripts/uninstall.sh | bash
```

Removes the binary, cleans up shell integration, and optionally removes the APM config directory.

## Quick start

### Import your current setup

```bash
apm create --current
```

This creates a "default" profile from your existing `~/.claude/` and activates it. New profiles always auto-activate, and one named "default" also becomes the global default for new shells.

If you run `apm create` without `--current` and no profiles exist yet, APM auto-imports from `~/.claude/` if it finds a `settings.json` there.

### Add more profiles

```bash
apm create work --description "Work environment"
apm create personal --from work  # clone an existing profile
```

### Switch profiles

```bash
# Activate in this shell
eval "$(apm use work)"

# Also set as default for new shells
eval "$(apm use work --global)"

# Deactivate
eval "$(apm use --unset)"
```

### Shell integration

Add this to your `~/.bashrc` or `~/.zshrc` so switching just works:

```bash
eval "$(apm init bash)"   # or: eval "$(apm init zsh)"
```

With shell integration, `apm use` works without the `eval "$(...)"` wrapper, and your default profile auto-activates in new shells.

Running `apm` with no arguments prints a status overview of your current setup.

## Commands

| Command | Aliases | Description |
|---------|---------|-------------|
| `apm create [name]` | | Create a profile (defaults to "default") |
| `apm use <profile>` | | Switch to a profile |
| `apm use --unset` | | Deactivate current profile |
| `apm ls` | `list` | List all profiles |
| `apm describe <name>` | | Show profile details and settings |
| `apm edit <name>` | | Open settings.json in `$EDITOR` |
| `apm delete <name>` | `rm` | Delete a profile |
| `apm diff <a> <b>` | | Show differences between profiles |
| `apm rename <old> <new>` | `mv` | Rename a profile |
| `apm doctor` | | Check for common issues |
| `apm nuke` | | Wipe all profiles, keep common |
| `apm current` | | Print active profile name |
| `apm init <bash\|zsh>` | | Print shell integration script |

Global flags: `--debug` (verbose logging), `--config-dir <path>` (custom config directory)

### `apm create`

```bash
apm create                              # "default", auto-activates
apm create work                         # empty "work", auto-activates
apm create work --current               # import from ~/.claude, auto-activates
apm create work --from personal         # copy existing profile, auto-activates
apm create work --description "My work" # with a description
```

### `apm use`

```bash
eval "$(apm use work)"            # activate in this shell
eval "$(apm use work --global)"   # also set as default for new shells
eval "$(apm use --unset)"         # deactivate
```

When piped through `eval`, `apm use` outputs shell exports (`APM_PROFILE`, `CLAUDE_CONFIG_DIR`, plus any profile env vars). Run it directly in a terminal and it prints human-readable instructions instead.

Switching profiles automatically unsets env vars from the previous profile before exporting new ones, so nothing leaks between profiles.

### `apm edit`

Opens settings.json in your editor (`$VISUAL` > `$EDITOR` > `vi`). Validates JSON on save and auto-regenerates the profile if it's active.

### Auto-regeneration

APM detects when source files change and regenerates on the next `apm use`. No manual step needed — just switch profiles or open a new shell.

Under the hood, APM hashes source file metadata (mtimes) and stores it in `.apm-meta.json`. On activation, it compares hashes and regenerates if they differ. The check is fast (~1ms, stat-only, no file reads).

`apm edit` also triggers regeneration if the edited profile is active.

### `apm nuke`

Wipes all profiles and generated data. Common stays intact.

If a profile is active, the generated directory gets flattened into `~/.claude` (symlinks become real files) so nothing breaks.

```bash
apm nuke           # asks for confirmation
apm nuke --force   # skip the prompt
```

## Environment variables

Each profile can define environment variables in an `env` file. These get exported when you switch to the profile and unset when you switch away.

**common/env** — shared across all profiles:
```bash
# Shared API settings
ANTHROPIC_MODEL=claude-sonnet-4-20250514
```

**profiles/bedrock/env** — profile-specific:
```bash
AWS_PROFILE=bedrock-account
AWS_REGION=us-west-2
AWS_BEDROCK_MODEL=us.anthropic.claude-sonnet-4-20250514
```

Env files are merged during generation (profile wins on conflicts). Format: `KEY=VALUE` per line, `#` for comments, optional quotes on values.

When switching profiles, old env vars are unset before new ones are exported.

## Settings merge

When generating a profile, `common/settings.json` is deep-merged with `profiles/<name>/settings.json`:

| Case | Behavior |
|------|----------|
| Scalars | Profile wins |
| Nested objects | Recursive deep merge |
| `permissions.allow` | Union (deduplicated) |
| `enabledPlugins` | Object merge (all keys kept) |
| Value set to `null` | Key gets deleted |
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

**Result:**
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

`voiceEnabled` was deleted (null sentinel), `permissions.allow` was unioned, `enabledPlugins` was object-merged.

## Managed directories

Profiles can include these directories. Files from common and profile are symlinked into the generated output, with profile files winning on conflicts:

- `skills/`
- `commands/`
- `agents/`
- `rules/`

Other files outside the managed set (like `CLAUDE.md`) also get symlinked in.

## Per-profile auth

APM isolates `.claude.json` per profile via `CLAUDE_CONFIG_DIR`. Each generated directory has its own `.claude.json`, so profiles can use different Claude accounts.

`apm create --current` copies your current `~/.claude.json` into the generated directory. Profiles created without `--current` start fresh.

On macOS, OAuth tokens live in the Keychain. `.claude.json` just records which account (`oauthAccount`) to use.

## Profile metadata

Each profile has a `profile.yaml`:
- `name` — profile name
- `description` — optional
- `created_at` — RFC 3339 timestamp
- `source` — `""`, `"current"`, or the name of the profile it was cloned from

## Profile names

Lowercase letters, numbers, and hyphens. Must start with a letter or number. Max 50 characters. Reserved: `common`, `generated`, `config`, `current`, `state`.

## Configuration

Data lives in `~/.config/apm/` (or `$XDG_CONFIG_HOME/apm/`).

**config.yaml:**
- `default_profile` — default for new shells
- `active_profile` — currently active profile
- `claude_dir` — custom `~/.claude` location

Override: `apm --config-dir /path/to/dir <command>`

## Development

```bash
just build       # or: make build
just test        # or: make test
just dev         # sandboxed shell (won't touch real ~/.claude)
just run ls      # one-off command in sandbox
just vet         # or: make vet
```

Dev mode uses the `dev` build tag and auto-sandboxes config to `/tmp/apm-dev-<uid>`.

## Requirements

- Go 1.25+ (building from source)
- bash or zsh
- macOS or Linux
