#!/bin/sh

event_name="$1"
raw_input="$(cat)"
project_dir="$(printf '%s' "$raw_input" | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"

if [ -z "$project_dir" ]; then
  project_dir="${CLAUDE_PROJECT_DIR:-$(pwd)}"
fi

find_project_root() {
  current="$1"
  while [ -n "$current" ]; do
    if [ -f "$current/.re-discipline/project-profile.md" ] || \
       [ -f "$current/.claude/project-profile.md" ] || \
       [ -f "$current/.codex/project-profile.md" ] || \
       [ -f "$current/docs/INDEX.md" ]; then
      printf '%s\n' "$current"
      return 0
    fi

    parent="$(dirname "$current")"
    if [ "$parent" = "$current" ]; then
      return 1
    fi
    current="$parent"
  done
  return 1
}

json_string_from_file() {
  awk '
    BEGIN { printf "\"" }
    {
      gsub(/\\/, "\\\\")
      gsub(/"/, "\\\"")
      gsub(/\t/, "\\t")
      gsub(/\r/, "\\r")
      printf "%s\\n", $0
    }
    END { printf "\"" }
  ' "$1"
}

json_string_from_text() {
  printf '%s\n' "$1" | awk '
    BEGIN { printf "\"" }
    {
      gsub(/\\/, "\\\\")
      gsub(/"/, "\\\"")
      gsub(/\t/, "\\t")
      gsub(/\r/, "\\r")
      if (NR > 1) {
        printf "\\n"
      }
      printf "%s", $0
    }
    END { printf "\"" }
  '
}

project_root="$(find_project_root "$project_dir")"
if [ -z "$project_root" ]; then
  exit 0
fi

canonical_profile="$project_root/.re-discipline/project-profile.md"
onboard_reminder='Reminder: invoke the re-discipline onboard skill before substantive work. Read the canonical project profile, active manager adapter, docs/INDEX.md, truth and history indexes, and any active CAMPAIGN.md.'
recovery_reminder='Legacy or incomplete re-discipline project detected without .re-discipline/project-profile.md. Invoke init-project in migration or recovery mode before substantive work; legacy host profiles are recovery input only.'
checkpoint_reminder='Reminder: context is about to compact. If a campaign is active, invoke checkpoint-campaign and preserve Current state plus dead ends. If it is solved, invoke close-campaign.'

case "$event_name" in
  session-start)
    if [ -n "${PLUGIN_ROOT:-}" ]; then
      printf '%s' '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":'
      if [ -f "$canonical_profile" ]; then
        json_string_from_file "$canonical_profile"
      else
        json_string_from_text "$recovery_reminder"
      fi
      printf '%s\n' '}}'
    elif [ -f "$canonical_profile" ]; then
      printf '%s\n' "$onboard_reminder"
    else
      printf '%s\n' "$recovery_reminder"
    fi
    ;;
  pre-compact)
    if [ -n "${PLUGIN_ROOT:-}" ]; then
      printf '%s' '{"systemMessage":'
      json_string_from_text "$checkpoint_reminder"
      printf '%s\n' '}'
    else
      printf '%s\n' "$checkpoint_reminder"
    fi
    ;;
  *)
    exit 0
    ;;
esac
