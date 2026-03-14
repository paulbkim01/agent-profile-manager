# Post-Build Handoff — Agent Profile Manager (apm)

## What was reviewed and polished (Phase 7)

Full review of all `cmd/*.go` and `internal/**/*.go` files for:

- **Error message formatting**: All user-facing errors now include actionable guidance (e.g. "Run 'apm ls' to see available profiles", "Use 'apm regenerate <name>' or --all").
- **Debug logging coverage**: Every cmd file now has `log.Printf` at key decision points. Combined with the existing logging in `internal/` packages (config, merge, generate, profile), running `apm --debug <command>` provides full traceability.
- **`main.go` error prefix**: Errors now print as `error: <message>` to stderr.
- **Shell completion**: `rootCmd.CompletionOptions.HiddenDefaultCmd = true` added to hide the default `completion` subcommand from help output while keeping it functional.
- **`errors.Is` consistency**: Replaced `os.IsNotExist(err)` with `errors.Is(err, os.ErrNotExist)` in tests (internal packages already used the correct form).
- **Describe output**: Managed directory entries now display as comma-separated lists instead of raw Go slice formatting.
- **Error return handling**: Verified all `DefaultProfile()` call sites handle the `(string, error)` return correctly. No discarded errors in any cmd or internal business logic.

## Full file inventory

### Entry point
- `main.go` -- entry point, error formatting

### Commands (`cmd/`)
| File | Command | Aliases |
|---|---|---|
| `root.go` | `apm` (root) | -- |
| `use.go` | `apm use <profile> [--global]` / `apm use --unset` | -- |
| `current.go` | `apm current` | -- |
| `init_shell.go` | `apm init <bash\|zsh>` | -- |
| `create.go` | `apm create <name> [--from] [--description]` | -- |
| `delete.go` | `apm delete <name> [--force]` | `rm` |
| `edit.go` | `apm edit <name>` | -- |
| `ls.go` | `apm ls` | `list` |
| `describe.go` | `apm describe <name>` | -- |
| `regenerate.go` | `apm regenerate [name] [--all]` | `regen` |
| `cmd_test.go` | 15 integration tests | -- |

### Internal packages (`internal/`)
| File | Purpose |
|---|---|
| `constants.go` | ManagedItems, ManagedDirs, ReservedNames, path defaults |
| `config/config.go` | Config loading, path resolution, config.yaml I/O |
| `validate/validate.go` | Profile name and settings.json validation |
| `validate/validate_test.go` | Validation tests |
| `merge/merge.go` | Deep JSON merge with special-case handling |
| `merge/merge_test.go` | 14 merge tests including realistic scenario |
| `profile/profile.go` | CRUD operations (create, delete, list, get, exists) |
| `profile/profile_test.go` | 13 profile tests including symlink/subdirectory import |
| `generate/generate.go` | Directory assembly (merge + symlinks) |
| `generate/generate_test.go` | 7 generation tests |
| `shell/shell.go` | Init script templates for bash/zsh |
| `shell/shell_test.go` | 4 shell tests |

### Other
- `go.mod`, `go.sum` -- dependencies
- `.gitignore` -- excludes `apm` binary
- `docs/plans/implementation-plan.md` -- architecture reference

## How to verify

```bash
# Build
go build -o apm .

# Run all tests (should be ~50+ tests across all packages)
go test ./...

# Quick smoke test
./apm create personal --description "personal projects"
./apm create work --from personal --description "work setup"
./apm ls
./apm describe work
eval "$(./apm use work)"
echo $APM_PROFILE       # work
echo $CLAUDE_CONFIG_DIR  # ~/.config/apm/generated/work
./apm current            # work
./apm --debug use work 2>&1 | head -20  # debug output
./apm regenerate --all
eval "$(./apm use --unset)"
./apm delete personal
./apm delete work --force
```

## Known issues / remaining tech debt

1. **`internal/constants.go` discards `os.UserHomeDir()` errors**: `DefaultClaudeDir()` and `DefaultAPMDir()` return `string` not `(string, error)`. If home dir resolution fails, downstream filesystem operations will error clearly, but the root cause won't be obvious. A future refactor could make these return errors.

2. **No tests for `edit.go`**: The edit command spawns an interactive editor process (`exec.Command`), which cannot be tested without mocking or subprocess patterns. Set `EDITOR=true` for manual smoke testing.

3. **No tests for `ls` active/default markers**: Testing the `*` marker and `(this shell)` / `(global default)` annotations requires setting env vars and config.yaml within the test harness.

4. **`describe` settings iteration order**: Uses `map[string]any` range, so settings key order is non-deterministic between runs.

5. **No `config/` package tests**: The config package is exercised indirectly through profile and cmd tests, but has no dedicated unit tests.

6. **Shell integration requires `eval`**: Users must wrap `apm use` in `eval` or use `apm init` shell integration. Running `apm use` directly on a TTY prints instructions but cannot modify the parent shell's environment.

## What a post-build reviewer should look at

- **Security**: The `generate` package creates symlinks and writes files. Verify symlink targets cannot escape the expected directories.
- **Edge cases**: Test with missing `~/.claude/` directory, empty profiles, profiles with only managed dirs, etc.
- **Cross-platform**: Currently tested on macOS. The symlink strategy may need adjustment for Windows.
- **Performance**: `apm current` should stay under ~5ms for statusline use. Profile with `time apm current`.
- **Shell integration**: Test both bash and zsh init scripts in real shells, especially the auto-activate behavior on shell startup.
