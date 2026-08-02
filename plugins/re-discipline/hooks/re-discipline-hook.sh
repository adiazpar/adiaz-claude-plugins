#!/bin/sh
set -eu

event=${1:-}
input=$(cat)

json_string() {
  key=$1
  printf '%s' "$input" | sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" | head -n 1
}

json_boolean() {
  key=$1
  printf '%s' "$input" | awk -v key="\"$key\"" '
    {
      pattern = key "[[:space:]]*:[[:space:]]*(true|false)"
      if (match($0, pattern)) {
        value = substr($0, RSTART, RLENGTH)
        sub(/^.*:[[:space:]]*/, "", value)
        print value
        exit
      }
    }
  '
}

escape_json() {
  printf '%s' "$1" | awk 'BEGIN { ORS="" } { gsub(/\\/, "\\\\"); gsub(/\"/, "\\\""); if (NR > 1) printf "\\n"; printf "%s", $0 }'
}

emit_context() {
  hook=$1
  context=$2
  printf '{"hookSpecificOutput":{"hookEventName":"%s","additionalContext":"%s"}}\n' "$hook" "$(escape_json "$context")"
}

emit_deny() {
  reason=$1
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}\n' "$(escape_json "$reason")"
}

find_root() {
  start=$1
  [ -n "$start" ] || start=$(pwd -P)
  if [ -f "$start" ]; then start=$(dirname "$start"); fi
  if ! current=$(cd "$start" 2>/dev/null && pwd -P); then current=$(pwd -P); fi
  while [ "$current" != / ]; do
    if [ -f "$current/.re-discipline/project-profile.md" ]; then printf '%s\n' "$current"; return; fi
    current=$(dirname "$current")
  done
  if [ -f "/.re-discipline/project-profile.md" ]; then printf '/\n'; fi
}

normalize_path() {
  awk -v value="$1" 'BEGIN {
    gsub(/\\/, "/", value); absolute=(substr(value,1,1)=="/");
    if (value ~ /^[A-Za-z]:\//) {
      value="/" tolower(substr(value,1,1)) substr(value,3);
      absolute=1;
    }
    n=split(value, parts, "/"); depth=0;
    for (i=1;i<=n;i++) {
      if (parts[i]=="" || parts[i]==".") continue;
      if (parts[i]=="..") { if (depth>0) depth--; continue; }
      stack[++depth]=parts[i];
    }
    out=absolute ? "/" : "";
    for (i=1;i<=depth;i++) { if (i>1) out=out "/"; out=out stack[i]; }
    print out;
  }'
}

relative_path() {
  path=$1
  root=$2
  parent=$(dirname "$path")
  leaf=$(basename "$path")
  if canonical_parent=$(cd "$parent" 2>/dev/null && pwd -P); then
    absolute="$canonical_parent/$leaf"
  else
    case "$path" in /*|[A-Za-z]:/*) absolute=$path ;; *) absolute="$root/$path" ;; esac
  fi
  absolute=$(normalize_path "$absolute")
  root_norm=$(normalize_path "$root")
  case "$absolute" in "$root_norm"/*) printf '%s\n' "${absolute#"$root_norm"/}" ;; *) printf '\n' ;; esac
}

protected_path() {
  rel=$1
  case "$rel" in
    active/*/runs/*/report.md|active/*/runs/*/payload/*) return 1 ;;
    active/*/campaign.json|active/*/STATE.md|active/*/work-items|active/*/work-items/*|active/*/runs|active/*/runs/*|active/*/findings|active/*/findings/*|active/*/intake|active/*/intake/*|active/*/reviews|active/*/reviews/*|active/*/events|active/*/events/*|active/*/closure|active/*/closure/*|docs/truth|docs/truth/*) return 0 ;;
    *) return 1 ;;
  esac
}

server_health() {
  root=$1
  plugin_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
  manifest="$plugin_root/knowledge/bin/manifest.json"
  runtime="$plugin_root/knowledge/bin/re-discipline-knowledge"
  if [ ! -f "$manifest" ] || [ ! -x "$runtime" ]; then printf 'runtime unavailable\n'; return; fi
  version=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$manifest" | head -n 1)
  if [ "$version" != 0.8.0 ]; then printf 'runtime version mismatch\n'; return; fi
  if "$runtime" preflight --asset-root "$plugin_root/knowledge" --project-root "$root" >/dev/null 2>&1; then
    printf 'preflight passed\n'
  else
    printf 'preflight needs attention\n'
  fi
}

cwd=$(json_string cwd)
[ -n "$cwd" ] || cwd=$(json_string projectRoot)
[ -n "$cwd" ] || cwd=$(pwd -P)
root=$(find_root "$cwd")

if [ "$event" = pre-tool-use ]; then
  tool=$(json_string tool_name)
  [ -n "$tool" ] || tool=$(json_string toolName)
  path=$(json_string file_path)
  [ -n "$path" ] || path=$(json_string filePath)
  [ -n "$path" ] || path=$(json_string path)
  if { [ "$tool" = Write ] || [ "$tool" = Edit ]; } && [ -n "$root" ]; then
    rel=$(relative_path "$path" "$root")
    if protected_path "$rel"; then
      emit_deny "Direct Write/Edit to '$rel' is blocked. Use the re-discipline shared state engine; use migrate-project only for prior-version inputs."
      exit 0
    fi
  fi
  printf '{}\n'
  exit 0
fi

if [ -z "$root" ]; then printf '{}\n'; exit 0; fi

campaign=$(json_string campaignId); [ -n "$campaign" ] || campaign=${RE_DISCIPLINE_CAMPAIGN_ID:-}
work_item=$(json_string workItemId); [ -n "$work_item" ] || work_item=${RE_DISCIPLINE_WORK_ITEM_ID:-}
generation=$(json_string generation); [ -n "$generation" ] || generation=${RE_DISCIPLINE_GENERATION_ID:-}
event_head=$(json_string lastEventId); [ -n "$event_head" ] || event_head=${RE_DISCIPLINE_LAST_EVENT_ID:-}
run_id=$(json_string runId); [ -n "$run_id" ] || run_id=${RE_DISCIPLINE_RUN_ID:-}
run_path=$(json_string runPath); [ -n "$run_path" ] || run_path=${RE_DISCIPLINE_RUN_PATH:-}
pack_digest=$(json_string contextPackDigest); [ -n "$pack_digest" ] || pack_digest=${RE_DISCIPLINE_CONTEXT_PACK_DIGEST:-}

case "$event" in
  session-start)
    health=$(server_health "$root")
    handles=none
    if [ -d "$root/active" ]; then
      handles=$(for record in "$root"/active/*/campaign.json; do
        [ -f "$record" ] || continue
        basename "$(dirname "$record")"
      done | sort | head -n 8 | paste -sd ', ' -)
      [ -n "$handles" ] || handles=none
    fi
    emit_context SessionStart "Re-discipline 0.8 project detected; server $health. Invoke onboard and call bounded state mode orient before substantive work. Active campaign handles: $handles. Canonical records are engine-owned; generated views and caches are derived."
    ;;
  pre-compact)
    emit_context PreCompact "No semantic save is required. Persist only an already-started atomic engine transaction. Resume handles: campaign=$campaign workItem=$work_item generation=$generation lastEvent=$event_head."
    ;;
  post-compact)
    emit_context PostCompact "Rehydrate with bounded state mode orient, then state mode resume for campaign=$campaign since generation=$generation or lastEvent=$event_head. Expand only cited handles needed for the next decision."
    ;;
  subagent-start)
    emit_context SubagentStart "Assigned run=$run_id workItem=$work_item path=$run_path contextPackDigest=$pack_digest. Read the exact brief and verify the immutable pack. Write only report.md, lazy payload/, and explicit project grants. Do not mutate canonical state or ratify findings."
    ;;
  subagent-stop)
    report_status='run path unavailable'
    if [ -n "$run_path" ]; then
      rel=$(relative_path "$run_path" "$root")
      case "$rel" in
        active/*/runs/*|.re-discipline/agents/recruiting/*/runs/*)
          if [ -f "$run_path/report.md" ]; then report_status='report present'; else report_status='report missing'; fi
          ;;
        *) report_status='run path outside registered run roots' ;;
      esac
    fi
    emit_context SubagentStop "Run return check: $report_status. Submit run.return through the shared engine to freeze the report digest and queue curation. Return does not imply review or ratification."
    ;;
  stop)
    in_flight=$(json_boolean transactionInFlight); [ -n "$in_flight" ] || in_flight=${RE_DISCIPLINE_TRANSACTION_IN_FLIGHT:-false}
    case "$in_flight" in
      true|1|yes) emit_context Stop 'A shared-engine transaction is reported in flight. Let it publish or recover atomically before ending; do not edit state files directly.' ;;
      *) printf '{}\n' ;;
    esac
    ;;
  *) printf '{}\n' ;;
esac
