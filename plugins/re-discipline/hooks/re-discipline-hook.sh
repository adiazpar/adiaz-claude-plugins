#!/bin/sh

event_name="$1"
raw_input="$(cat)"
project_dir="$(printf '%s' "$raw_input" | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"

if [ -z "$project_dir" ]; then
  project_dir="${CLAUDE_PROJECT_DIR:-$(pwd)}"
fi

if [ ! -f "$project_dir/.re-discipline/project-profile.md" ] && \
   [ ! -f "$project_dir/.claude/project-profile.md" ] && \
   [ ! -f "$project_dir/docs/INDEX.md" ]; then
  exit 0
fi

case "$event_name" in
  session-start)
    printf '%s\n' 'Reminder: invoke the re-discipline onboard skill before substantive work. Read the canonical project profile, docs/INDEX.md, truth and history indexes, and any active CAMPAIGN.md.'
    ;;
  pre-compact)
    printf '%s\n' 'Reminder: context is about to compact. If a campaign is active, invoke checkpoint-campaign and preserve Current state plus dead ends. If it is solved, invoke close-campaign.'
    ;;
  *)
    exit 0
    ;;
esac
