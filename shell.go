package main

import "fmt"

// shellInitTemplate is the shared shell integration script.
// %[1]s = open bracket ([ or [[), %[2]s = close bracket (] or ]])
const shellInitTemplate = `
# Agent Profile Manager shell integration
apm() {
  case "$1" in
    use)
      local stdout_file stderr_file
      stdout_file=$(mktemp) stderr_file=$(mktemp)
      command apm "$@" >"$stdout_file" 2>"$stderr_file"
      local rc=$?
      if %[1]s $rc -eq 0 %[2]s; then
        eval "$(cat "$stdout_file")"
        rc=$?
      fi
      cat "$stderr_file" >&2
      rm -f "$stdout_file" "$stderr_file"
      return $rc
      ;;
    *)
      command apm "$@"
      ;;
  esac
}

# Auto-activate global default on shell startup
_apm_auto_activate() {
  if %[1]s -z "$APM_PROFILE" %[2]s; then
    local profile
    profile=$(command apm current 2>/dev/null)
    if %[1]s -n "$profile" %[2]s; then
      export APM_PROFILE="$profile"
    fi
  fi
}
_apm_auto_activate
`

var (
	bashInit = fmt.Sprintf(shellInitTemplate, "[", "]")
	zshInit  = fmt.Sprintf(shellInitTemplate, "[[", "]]")
)

// shellInitScript returns the shell integration script for the given shell.
func shellInitScript(shellType string) (string, error) {
	switch shellType {
	case "bash":
		return bashInit, nil
	case "zsh":
		return zshInit, nil
	default:
		return "", fmt.Errorf("unsupported shell: %s (use 'bash' or 'zsh')", shellType)
	}
}
