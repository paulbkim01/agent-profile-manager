package shell

import "fmt"

const bashInit = `
# Agent Profile Manager shell integration
apm() {
  case "$1" in
    use)
      local stdout_file stderr_file
      stdout_file=$(mktemp) stderr_file=$(mktemp)
      command apm "$@" >"$stdout_file" 2>"$stderr_file"
      local rc=$?
      if [ $rc -eq 0 ]; then
        eval "$(cat "$stdout_file")"
      else
        cat "$stderr_file" >&2
      fi
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
  if [ -z "$APM_PROFILE" ]; then
    local default_profile
    default_profile=$(command apm current 2>/dev/null)
    if [ -n "$default_profile" ]; then
      eval "$(command apm use "$default_profile" 2>/dev/null)"
    fi
  fi
}
_apm_auto_activate
`

const zshInit = `
# Agent Profile Manager shell integration
apm() {
  case "$1" in
    use)
      local stdout_file stderr_file
      stdout_file=$(mktemp) stderr_file=$(mktemp)
      command apm "$@" >"$stdout_file" 2>"$stderr_file"
      local rc=$?
      if [[ $rc -eq 0 ]]; then
        eval "$(cat "$stdout_file")"
      else
        cat "$stderr_file" >&2
      fi
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
  if [[ -z "$APM_PROFILE" ]]; then
    local default_profile
    default_profile=$(command apm current 2>/dev/null)
    if [[ -n "$default_profile" ]]; then
      eval "$(command apm use "$default_profile" 2>/dev/null)"
    fi
  fi
}
_apm_auto_activate
`

// InitScript returns the shell integration script for the given shell.
func InitScript(shellType string) (string, error) {
	switch shellType {
	case "bash":
		return bashInit, nil
	case "zsh":
		return zshInit, nil
	default:
		return "", fmt.Errorf("unsupported shell: %s (use 'bash' or 'zsh')", shellType)
	}
}
