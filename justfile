bin := "bin/apm"
dev_bin := "bin/apm-dev"
version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`

# List available recipes
default:
    @just --list

# Build the binary
build:
    go build -ldflags "-X main.version={{ version }}" -o {{ bin }} .

# Run tests with race detector
test:
    go test -count=1 -timeout 120s -race ./...

# Run go vet
vet:
    go vet ./...

# Lint (currently just vet)
lint: vet

# Remove build artifacts
clean:
    rm -rf bin/

# Install to GOPATH
install: build
    cp {{ bin }} "${GOPATH:-$HOME/go}/bin/apm"

# Run all checks
all: vet test build

# Build and launch a shell with apm on PATH against real ~/.config/apm
start: build
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="{{ justfile_directory() }}/bin:$PATH"
    printf '=== APM (real environment) ===\n'
    printf 'Binary: %s\n' "{{ bin }}"
    printf 'Config: %s\n\n' "${XDG_CONFIG_HOME:-$HOME/.config}/apm"
    "${SHELL:-bash}"

# Build and run a one-off command with dev tag (sandboxed by default)
run *args: (_dev-build)
    @{{ dev_bin }} {{ args }}

# Interactive dev mode — sandboxed config, cleanup on exit
dev:
    #!/usr/bin/env bash
    set -euo pipefail

    DEV_DIR=$(mktemp -d "${TMPDIR:-/tmp}/apm-dev.XXXXXX")

    cleanup() {
        rc=$?
        printf '\nCleaning up dev environment...\n'
        rm -rf "$DEV_DIR"
        rm -f "{{ dev_bin }}"
        printf 'Dev residuals removed.\n'
        exit $rc
    }
    trap cleanup EXIT INT TERM

    go build -tags dev -o "{{ dev_bin }}" .

    printf '=== APM Dev Mode ===\n'
    printf 'Config:  %s\n' "$DEV_DIR"
    printf 'Binary:  %s\n\n' "{{ dev_bin }}"
    printf 'The dev binary auto-sandboxes to the temp config dir.\n'
    printf 'Run commands directly:  apm-dev create work --from current\n'
    printf 'Type "exit" or Ctrl+C to stop and cleanup.\n\n'

    export PATH="{{ justfile_directory() }}/bin:$PATH"
    "${SHELL:-bash}"

# Build the dev binary (internal recipe)
[private]
_dev-build:
    @go build -tags dev -o {{ dev_bin }} .
