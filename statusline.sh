#!/bin/bash
# apm — default Claude Code status line
# Shared across all profiles via common/ symlink.
# Override per-profile by placing a copy in profiles/<name>/statusline.sh.
# Requires: jq (https://jqlang.github.io/jq/)

input=$(cat)

if ! command -v jq >/dev/null 2>&1; then
  echo "[apm] install jq for status line"
  exit 0
fi

# ── External info ─────────────────────────────────────────────────

user="${USER:-$(whoami 2>/dev/null || echo unknown)}"
profile="${APM_PROFILE:-none}"

cwd=$(echo "$input" | jq -r '.cwd // empty')
git_branch=""
git_root=""
if [ -n "$cwd" ]; then
  git_branch=$(git -C "$cwd" rev-parse --abbrev-ref HEAD 2>/dev/null || true)
  git_root_full=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null || true)
  [ -n "$git_root_full" ] && git_root=$(basename "$git_root_full")
fi

effort=""
for f in "${CLAUDE_CONFIG_DIR:-__none__}/settings.json" \
         "${CLAUDE_CONFIG_DIR:-__none__}/settings.local.json" \
         "$HOME/.claude/settings.json" \
         "$HOME/.claude/settings.local.json"; do
  if [ -f "$f" ]; then
    e=$(jq -r '.effortLevel // empty' "$f" 2>/dev/null || true)
    [ -n "$e" ] && effort="$e"
  fi
done

# ── Colors ────────────────────────────────────────────────────────

if [ -z "${NO_COLOR:-}" ]; then
  R=$'\033[0m' B=$'\033[1m' D=$'\033[2m'
  CY=$'\033[36m' GR=$'\033[32m' YL=$'\033[33m'
  RD=$'\033[31m' BL=$'\033[34m' MG=$'\033[35m'
else
  R="" B="" D="" CY="" GR="" YL="" RD="" BL="" MG=""
fi

# ── Format ────────────────────────────────────────────────────────

echo "$input" | jq -r \
  --arg user "$user" \
  --arg profile "$profile" \
  --arg git_branch "$git_branch" \
  --arg git_root "$git_root" \
  --arg effort "$effort" \
  --arg R "$R" --arg B "$B" --arg D "$D" \
  --arg CY "$CY" --arg GR "$GR" --arg YL "$YL" \
  --arg RD "$RD" --arg BL "$BL" --arg MG "$MG" \
'
def fmt_time:
  (. / 1000 | floor) as $s |
  ($s / 3600 | floor) as $h |
  (($s % 3600) / 60 | floor) as $m |
  ($s % 60) as $sec |
  if $h > 0 then "\($h)h\($m)m"
  elif $m > 0 then "\($m)m\($sec)s"
  else "\($sec)s"
  end;

def fmt_tokens:
  if . >= 1000000 then "\(. / 1000000 * 10 | floor / 10)M"
  elif . >= 1000 then "\(. / 1000 | floor)k"
  else tostring
  end;

def fmt_cost:
  (. // 0) | tostring |
  if test("[.]") then
    split(".") | "\(.[0]).\((.[1] + "00")[:2])"
  else . + ".00"
  end |
  "$" + .;

def bar:
  (. // 0) as $pct |
  ($pct / 10 | floor) as $filled |
  (10 - $filled) as $empty |
  (if $pct >= 80 then $RD
   elif $pct >= 50 then $YL
   else $GR end) +
  ("█" * $filled) + $D + ("░" * $empty) +
  $R + " " +
  (if $pct >= 80 then $RD + $B else "" end) +
  ($pct | tostring) + "%" + $R;

def effort_color:
  if . == "low" then $D
  elif . == "high" then $YL
  else "" end;

def s: $D + " │ " + $R;

# Line 1: user │ model │ effort │ bar pct │ cost │ time
([
  $B + $user + $R,
  $CY + (.model.display_name // "?") + $R,
  (if $effort != "" then
    ($effort | effort_color) + $effort + $R
  else null end),
  ((.context_window.used_percentage // 0) | bar),
  $YL + (.cost.total_cost_usd | fmt_cost) + $R,
  ((.cost.total_duration_ms // 0) | fmt_time)
] | map(select(. != null)) | join(s)),

# Line 2: repo(branch) │ profile │ tokens │ vim │ worktree
([
  (if $git_root != "" then
    $B + $git_root + $R +
    (if $git_branch != "" then
      $D + "(" + $R + $GR + $git_branch + $R + $D + ")" + $R
    else "" end)
  else
    (.cwd // "") | split("/") | last
  end),
  $MG + "◆" + $R + " " + $B + $profile + $R,
  $D + "↑" + $R + ((.context_window.total_input_tokens // 0) | fmt_tokens) +
    $D + " ↓" + $R + ((.context_window.total_output_tokens // 0) | fmt_tokens),
  (if .vim.mode then $YL + $B + .vim.mode + $R else null end),
  (if .worktree.name then $BL + "⎇ " + .worktree.name + $R else null end)
] | map(select(. != null)) | join(s))
'
