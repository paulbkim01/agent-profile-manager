# Task 1: Polish -- debug logging, error messages, build

## Files to modify
- All `cmd/*.go` files (consistent error formatting)
- All `internal/` packages (debug logging)
- `main.go` (error output)

## Dependencies
- All previous phases

## Implementation

### main.go -- error formatting

Update main.go to print errors with color:

```go
package main

import (
	"fmt"
	"os"

	"github.com/paulbkim/agent-profile-manager/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
```

### Debug logging pattern

All `internal/` packages already use `log.Printf`. The root command's `PersistentPreRun` routes `log` output to either stderr (debug) or discard (normal). No changes needed to internal packages -- just verify coverage.

Debug output should cover:
- `config.go`: path resolution, config file loading
- `merge.go`: every merge decision (override, union, delete, recurse)
- `generate.go`: every symlink created, every dir merged
- `profile.go`: create source, delete cleanup

Example debug session:
```
$ apm --debug use work
2026/03/14 22:00:01 root.go:25: debug mode enabled
2026/03/14 22:00:01 config.go:45: config: apm_dir=/Users/paul/.config/apm claude_dir=/Users/paul/.claude
2026/03/14 22:00:01 generate.go:30: generate: building /Users/paul/.config/apm/generated/work
2026/03/14 22:00:01 merge.go:52: merge: override model
2026/03/14 22:00:01 merge.go:45: merge: union permissions.allow (7 items)
2026/03/14 22:00:01 merge.go:55: merge: object-merge enabledPlugins
2026/03/14 22:00:01 generate.go:85: generate: link skills/find-skills -> profile
2026/03/14 22:00:01 generate.go:115: generate: link history.jsonl -> shared
2026/03/14 22:00:01 generate.go:115: generate: link plugins -> shared
export APM_PROFILE=work
export CLAUDE_CONFIG_DIR=/Users/paul/.config/apm/generated/work
```

### Error message patterns

Errors should tell the user what to do:

```go
// Bad:
return fmt.Errorf("profile not found")

// Good:
return fmt.Errorf("profile '%s' not found. Run 'apm ls' to see available profiles", name)

// Bad:
return fmt.Errorf("cannot delete active profile")

// Good:
return fmt.Errorf("profile '%s' is the global default. Use --force to delete anyway", name)

// Bad:
return fmt.Errorf("invalid name")

// Good:
return fmt.Errorf("profile name must be lowercase alphanumeric with hyphens (e.g. 'my-work')")
```

Review all `RunE` functions and `internal/` error returns to follow this pattern.

### Build and test

```bash
# Build
go build -o apm .

# Run all tests
go test ./...

# Install globally
go install .

# Verify binary size (should be ~5-8MB)
ls -lh apm
```

### Shell completion (bonus)

Cobra has built-in completion support. Add to root.go init():

```go
func init() {
	// ... existing flags ...

	// Enable shell completions
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
}
```

Users can then run:
```bash
apm completion bash > /usr/local/etc/bash_completion.d/apm
apm completion zsh > "${fpath[1]}/_apm"
```

## Verification

Full smoke test:

```bash
go build -o apm .

# Create profiles
./apm create personal --description "side projects"
./apm create work --from current --description "EAK"

# List
./apm ls

# Switch (per-shell)
eval "$(./apm use work)"
echo $APM_PROFILE          # work
echo $CLAUDE_CONFIG_DIR    # ~/.config/apm/generated/work

# Check statusline
./apm current              # work

# Switch again
eval "$(./apm use personal)"
echo $APM_PROFILE          # personal

# Global default
eval "$(./apm use work --global)"

# Describe
./apm describe work

# Debug mode
./apm --debug use work 2>&1 | head -20

# Edit
./apm edit work

# Regenerate
./apm regenerate --all

# Unset
eval "$(./apm use --unset)"
echo $APM_PROFILE          # (empty)

# Delete
./apm delete personal
./apm delete work --force

# Shell integration
./apm init bash
./apm init zsh
```
