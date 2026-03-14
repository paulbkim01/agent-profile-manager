# Agent Profile Manager (apm) - Implementation Plan

## Context

Paul switches between personal (direct Anthropic API) and work (AWS Bedrock via SSO) Claude Code profiles. Today this means manually swapping `~/.claude/settings.json` with backup files like `settings.json.work`. Each context needs different env vars, model, permissions, plugins, skills, and statusline config. This tool automates that into a single command.

Claude Code respects the `CLAUDE_CONFIG_DIR` environment variable (defaults to `~/.claude`). This enables per-shell profile isolation without ever modifying `~/.claude/` directly.

---

## Architecture

### Core principle: never modify `~/.claude/`

APM builds generated profile directories that serve as complete `CLAUDE_CONFIG_DIR` targets. Each generated directory contains:
- A merged `settings.json` (common base + profile overrides, deep-merged)
- Merged directories for skills/, commands/, agents/ (symlinks from common + profile sources)
- Symlinks to `~/.claude/` for everything else (history, sessions, plugins, projects, cache, etc.)

`~/.claude/` remains the untouched fallback when no profile is active.

### Directory layout

```
~/.config/apm/                    # ($XDG_CONFIG_HOME/apm/)
├── config.yaml                   # global default profile, claude_dir override
├── common/                       # shared base layer
│   ├── settings.json             # common settings entries
│   ├── skills/                   # shared skills
│   ├── commands/                 # shared commands
│   └── agents/                   # shared agents
├── profiles/
│   ├── personal/
│   │   ├── profile.yaml          # metadata (name, description, created_at)
│   │   ├── settings.json         # profile-specific overrides only
│   │   ├── skills/
│   │   ├── commands/
│   │   └── agents/
│   └── work/
│       └── ...
└── generated/                    # assembled profiles (each = valid CLAUDE_CONFIG_DIR)
    └── work/
        ├── settings.json         # WRITTEN: deep merge of common + work
        ├── settings.local.json   # → ~/.claude/settings.local.json
        ├── skills/               # DIR: symlinks from common + work skills
        ├── commands/             # DIR: symlinks from common + work commands
        ├── agents/               # DIR: symlinks from common + work agents
        ├── plugins/              # → ~/.claude/plugins/
        ├── projects/             # → ~/.claude/projects/
        ├── history.jsonl         # → ~/.claude/history.jsonl
        ├── sessions/             # → ~/.claude/sessions/
        └── ...                   # everything else in ~/.claude/ → symlinked
```

### Switching

Per-shell (default): `eval "$(apm use work)"`
- Regenerates `~/.config/apm/generated/work/`
- Outputs: `export APM_PROFILE=work; export CLAUDE_CONFIG_DIR=~/.config/apm/generated/work`

Global: `eval "$(apm use work --global)"`
- Same generation step
- Also writes `default_profile: work` to `config.yaml`
- New shells auto-activate via shell init function

Shell integration: `eval "$(apm init bash)"` in `.bashrc`/`.zshrc` provides:
- A shell function wrapping `apm use` to auto-eval its output
- Auto-activation on shell startup based on global default from `config.yaml`

### Deep JSON merge strategy

| Type | Behavior |
|---|---|
| Scalars | Profile overrides common |
| Objects | Recursive merge (profile keys win, common-only keys preserved) |
| Arrays (general) | Profile replaces common entirely |
| `permissions.allow` | Union (deduplicated concat) |
| `enabledPlugins` | Object merge (both sides preserved, profile wins on conflict) |
| Value set to `null` | Removes that key from merged output (deletion sentinel) |

### Generation strategy

Whitelist for profile-managed items (settings.json, skills/, commands/, agents/). Everything else found in `~/.claude/` gets symlinked automatically. This makes the tool resilient to future Claude Code additions.

---

## Commands

| Command | Description |
|---|---|
| `apm use <profile> [--global]` | Switch profile. Per-shell by default, `--global` sets default for new shells |
| `apm use --unset` | Deactivate profile in current shell |
| `apm ls` | List profiles with active indicator |
| `apm create <name> [--from current\|<profile>] [--description TEXT]` | Create profile. No `--from` = empty profile |
| `apm delete <name> [--force]` | Delete profile. Refuses if active unless `--force` |
| `apm edit <name>` | Open profile's settings.json in `$EDITOR`, validate + regenerate on save |
| `apm describe <name>` | Show profile metadata, settings overrides, skills/commands/agents |
| `apm current` | Output active profile name (for statusline scripts) |
| `apm init <bash\|zsh>` | Output shell integration code |
| `apm regenerate [<name>\|--all]` | Rebuild generated directories |

Global flags: `--debug`, `--config-dir PATH`

### `apm current` (for statusline)

~5ms startup. No shell workaround needed.

```bash
# In statusline.sh:
profile=$(apm current 2>/dev/null)
[ -n "$profile" ] && printf "[%s] " "$profile"
```

Resolution: `$APM_PROFILE` env var > `config.yaml` default > exit 1.

---

## Project structure

```
agent-profile-manager/
├── go.mod
├── go.sum
├── main.go                       # entry point
├── cmd/
│   ├── root.go                   # cobra root command, --debug, --config-dir
│   ├── use.go                    # switch profile (per-shell + global)
│   ├── create.go                 # create profile
│   ├── delete.go                 # delete profile
│   ├── edit.go                   # open in $EDITOR, validate, regenerate
│   ├── ls.go                     # list profiles
│   ├── describe.go               # show profile details
│   ├── current.go                # output active profile name
│   ├── init_shell.go             # shell integration output
│   └── regenerate.go             # rebuild generated dirs
├── internal/
│   ├── config/
│   │   └── config.go             # ApmConfig, path resolution, config.yaml I/O
│   ├── merge/
│   │   ├── merge.go              # deep JSON merge (pure function)
│   │   └── merge_test.go         # exhaustive tests
│   ├── generate/
│   │   ├── generate.go           # directory assembly (merge + symlinks)
│   │   └── generate_test.go
│   ├── profile/
│   │   ├── profile.go            # CRUD operations
│   │   └── profile_test.go
│   ├── shell/
│   │   ├── shell.go              # init script templates for bash/zsh
│   │   └── shell_test.go
│   ├── validate/
│   │   ├── validate.go           # profile name validation
│   │   └── validate_test.go
│   └── constants.go              # paths, reserved names, managed items whitelist
└── cmd/
    └── cmd_test.go               # integration tests
```

### Dependencies

```
module github.com/paulbkim/agent-profile-manager

go 1.22

require (
    github.com/spf13/cobra v1.8.0
    github.com/charmbracelet/lipgloss v1.0.0
    gopkg.in/yaml.v3 v3.0.1
)
```

`go build -o apm .` produces a single binary. `go install` for distribution.

---

## Implementation sequence

### Phase 1: Foundation
1. `go.mod` + `main.go` + `cmd/root.go` (cobra root with `--debug`, `--config-dir`)
2. `internal/constants.go` -- path defaults, reserved names, managed items whitelist
3. `internal/config/config.go` -- config loading, path resolution (`$XDG_CONFIG_HOME`, `$CLAUDE_CONFIG_DIR`)
4. `internal/validate/validate.go` -- profile name validation (`^[a-z0-9][a-z0-9-]*$`, max 50 chars, reject reserved)
5. `internal/validate/validate_test.go`

### Phase 2: Merge engine
6. `internal/merge/merge.go` -- deep JSON merge with special-case handling
7. `internal/merge/merge_test.go` -- exhaustive tests (scalar override, nested merge, array replace, permissions union, enabledPlugins merge, null sentinel, empty inputs)

### Phase 3: Profile CRUD
8. `internal/profile/profile.go` -- create (with `--from`), delete (with active-profile safety), list, describe
9. `internal/profile/profile_test.go`

### Phase 4: Generation engine
10. `internal/generate/generate.go` -- directory assembly: merged settings.json, merged skill/command/agent dirs, auto-symlink everything else from `~/.claude/`
11. `internal/generate/generate_test.go`

### Phase 5: Switching + shell integration
12. `cmd/use.go` -- per-shell env var output, global default storage, unset
13. `internal/shell/shell.go` -- init script templates for bash/zsh
14. `cmd/init_shell.go` + `cmd/current.go`
15. `internal/shell/shell_test.go`

### Phase 6: CLI wiring
16. Remaining command files: `cmd/create.go`, `cmd/delete.go`, `cmd/edit.go`, `cmd/ls.go`, `cmd/describe.go`, `cmd/regenerate.go`
17. `cmd/cmd_test.go` -- integration tests

### Phase 7: Polish
18. Debug mode logging throughout (stderr via `log` package)
19. Error messages with actionable guidance
20. `go build` + manual smoke test

---

## Edge cases

- **Stale generated dirs**: always regenerate on `apm use`. Store content hash in `.apm-meta.json` for staleness detection.
- **Delete active profile**: refuse without `--force`. With `--force`: clear default, print shell cleanup instructions.
- **Missing `~/.claude/`**: skip symlinks for items that don't exist.
- **Concurrent shells**: each shell has its own `CLAUDE_CONFIG_DIR`. No locking needed.
- **Broken symlinks**: `apm regenerate` detects and fixes dangling symlinks.
- **Profile name collisions**: reject reserved names (common, generated, config, default).
- **`apm use` without eval**: detect non-eval invocation (check if stdout is a TTY) and print instructions.
- **Global switch while per-shell active**: per-shell `$CLAUDE_CONFIG_DIR` takes precedence.
- **`settings.local.json`**: stays outside profiles. Symlinked into generated dirs from `~/.claude/`.
- **Atomic writes**: write merged `settings.json` to temp file then `os.Rename()`.

---

## Verification

1. **Unit tests**: `go test ./...` -- merge, generation, CRUD, validation
2. **Manual smoke test**:
   - `apm create personal --from current`
   - `apm create work --from current` then `apm edit work`
   - `apm ls`
   - `eval "$(apm use personal)"` then `echo $CLAUDE_CONFIG_DIR`
   - `eval "$(apm use work)"` then `claude --version`
   - `apm current`
   - `apm describe work`
   - Two terminals, different profiles, verify isolation
3. **Statusline**: update `statusline.sh` to call `apm current`
