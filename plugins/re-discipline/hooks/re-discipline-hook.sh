#!/bin/sh
set -eu

event=${1:-}
input=$(cat)

json_object_string() {
  container=$1
  requested=$2
  printf '%s' "$input" | awk -v container="$container" -v requested="$requested" '
    function parse_string(text, start,    i, char, escaped, out) {
      parsed_ok = 0
      parsed_end = start
      if (substr(text, start, 1) != "\"") return ""
      escaped = 0
      out = ""
      for (i = start + 1; i <= length(text); i++) {
        char = substr(text, i, 1)
        if (escaped) {
          if (char == "n") out = out "\n"
          else if (char == "r") out = out "\r"
          else if (char == "t") out = out "\t"
          else if (char == "b") out = out sprintf("%c", 8)
          else if (char == "f") out = out sprintf("%c", 12)
          else if (char == "u") {
            if (i + 4 > length(text)) return ""
            out = out "\\u" substr(text, i + 1, 4)
            i += 4
          }
          else out = out char
          escaped = 0
        }
        else if (char == "\\") escaped = 1
        else if (char == "\"") {
          parsed_ok = 1
          parsed_end = i
          return out
        }
        else out = out char
      }
      return ""
    }
    function skip_space(text, start,    i, char) {
      for (i = start; i <= length(text); i++) {
        char = substr(text, i, 1)
        if (char !~ /[[:space:]]/) return i
      }
      return length(text) + 1
    }
    function find_object(text, object_start, key,    depth, arrays, i, char, token, end, colon, value_start) {
      located = 0
      depth = 0
      arrays = 0
      for (i = object_start; i <= length(text); i++) {
        char = substr(text, i, 1)
        if (char == "\"") {
          token = parse_string(text, i)
          if (!parsed_ok) return 0
          end = parsed_end
          if (depth == 1 && arrays == 0) {
            colon = skip_space(text, end + 1)
            if (substr(text, colon, 1) == ":") {
              value_start = skip_space(text, colon + 1)
              if (token == key && substr(text, value_start, 1) == "{") {
                located = 1
                return value_start
              }
            }
          }
          i = end
        }
        else if (char == "{") depth++
        else if (char == "}") {
          depth--
          if (depth == 0) return 0
          if (depth < 0) return 0
        }
        else if (char == "[") arrays++
        else if (char == "]") arrays--
      }
      return 0
    }
    function find_string(text, object_start, key,    depth, arrays, i, char, token, end, colon, value_start, value) {
      found = 0
      depth = 0
      arrays = 0
      for (i = object_start; i <= length(text); i++) {
        char = substr(text, i, 1)
        if (char == "\"") {
          token = parse_string(text, i)
          if (!parsed_ok) return ""
          end = parsed_end
          if (depth == 1 && arrays == 0) {
            colon = skip_space(text, end + 1)
            if (substr(text, colon, 1) == ":") {
              value_start = skip_space(text, colon + 1)
              if (token == key && substr(text, value_start, 1) == "\"") {
                value = parse_string(text, value_start)
                if (!parsed_ok) return ""
                found = 1
                return value
              }
            }
          }
          i = end
        }
        else if (char == "{") depth++
        else if (char == "}") {
          depth--
          if (depth == 0) return ""
          if (depth < 0) return ""
        }
        else if (char == "[") arrays++
        else if (char == "]") arrays--
      }
      return ""
    }
    {
      if (NR > 1) source = source "\n"
      source = source $0
    }
    END {
      root = skip_space(source, 1)
      if (substr(source, root, 1) != "{") exit 1
      object_start = root
      if (container != "") {
        object_start = find_object(source, root, container)
        if (!located) exit 1
      }
      value = find_string(source, object_start, requested)
      if (!found) exit 1
      printf "%s", value
    }
  '
}

json_string() {
  json_object_string '' "$1" || true
}

# Parse only top-level envelope identity, independent of JSON property order.
# JSON-looking strings nested in tool_input therefore cannot impersonate it.
json_envelope_string() {
  json_object_string '' "$1" || true
}

# Decode only a direct string child of tool_input. The parser never evaluates
# patch text, so embedded quotes and newlines cannot become shell syntax.
json_tool_string() {
  if json_object_string tool_input "$1"; then return 0; fi
  json_object_string toolInput "$1"
}

json_tool_command() {
  json_tool_string command
}

patch_targets() {
  printf '%s\n' "$1" | awk '
    { sub(/\r$/, "") }
    $0 == "*** Begin Patch" { begin_count++; inside = 1; ended = 0; next }
    $0 == "*** End Patch" { end_count++; inside = 0; ended = 1; next }
    /^\*\*\* (Add|Update|Delete) File:/ {
      if (!inside || ended) invalid = 1
      target = $0
      sub(/^\*\*\* (Add|Update|Delete) File:[[:space:]]*/, "", target)
      if (target == "") invalid = 1
      else { print target; target_count++ }
      next
    }
    /^\*\*\* Move to:/ {
      if (!inside || ended) invalid = 1
      target = $0
      sub(/^\*\*\* Move to:[[:space:]]*/, "", target)
      if (target == "") invalid = 1
      else { print target; target_count++ }
      next
    }
    /^\*\*\* .* (File|to):/ { invalid = 1 }
    END {
      if (begin_count != 1 || end_count != 1 || target_count == 0 || invalid) exit 1
    }
  '
}

valid_patch_target() {
  target=$1
  [ -n "$target" ] || return 1
  normalized=$(printf '%s' "$target" | tr '\\' '/')
  case "$normalized" in */|*[\<\>\"\|\?\*]*) return 1 ;; esac
  if printf '%s' "$normalized" | LC_ALL=C grep -q '[[:cntrl:]]'; then return 1; fi
  printf '%s\n' "$normalized"
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
  printf '%s' "$1" | awk 'BEGIN { ORS="" } { gsub(/\\/, "\\\\"); gsub(/"/, "\\\""); if (NR > 1) printf "\\n"; printf "%s", $0 }'
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
  case "$path" in /*|[A-Za-z]:/*) candidate=$path ;; *) candidate="$root/$path" ;; esac
  probe=$(dirname "$candidate")
  suffix=$(basename "$candidate")
  while [ ! -d "$probe" ]; do
    parent=$(dirname "$probe")
    [ "$parent" != "$probe" ] || break
    suffix="$(basename "$probe")/$suffix"
    probe=$parent
  done
  if canonical_parent=$(cd "$probe" 2>/dev/null && pwd -P); then
    absolute="$canonical_parent/$suffix"
  else
    absolute=$candidate
  fi
  absolute=$(normalize_path "$absolute")
  root_norm=$(normalize_path "$root")
  case "$absolute" in "$root_norm"/*) printf '%s\n' "${absolute#"$root_norm"/}" ;; *) printf '\n' ;; esac
}

protected_path() {
  rel=$1
  case "$rel" in
    active/*/campaign.json|active/*/state.md|active/*/work-items|active/*/work-items/*|active/*/runs|active/*/runs/*|active/*/findings|active/*/findings/*|active/*/intake|active/*/intake/*|active/*/reviews|active/*/reviews/*|active/*/events|active/*/events/*|active/*/closure|active/*/closure/*|docs/truth|docs/truth/*|docs/history/campaigns|docs/history/campaigns/*|.re-discipline/state|.re-discipline/state/*|.re-discipline/migration/0.8|.re-discipline/migration/0.8/*|.re-discipline/knowledge/migration|.re-discipline/knowledge/migration/*|.re-discipline/knowledge/normalization-queue.json|.re-discipline/knowledge/normalization-queue.json.lock|.re-discipline/knowledge/.re-discipline-tmp-*) return 0 ;;
    *) return 1 ;;
  esac
}

legacy_canonical_path() {
  rel=$1
  case "$rel" in
    active/*/campaign.md|active/*/reviews.md|.re-discipline/config.json|.re-discipline/knowledge/policy.jsonc|.re-discipline/knowledge/retrieval-profile.json) return 0 ;;
    *) return 1 ;;
  esac
}

project_version() {
  root=$1
  plugin_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
  runtime="$plugin_root/knowledge/bin/re-discipline-knowledge"
  if [ -x "$runtime" ] && result=$("$runtime" project-version --project-root "$root" 2>/dev/null); then
    version=$(printf '%s' "$result" | sed -n 's/.*"projectStateVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
    case "$version" in 0.7|0.8) printf '%s\n' "$version"; return ;; esac
  fi

  # Bounded read-only fallback for a stale or unavailable packaged runtime.
  # Only an unambiguous shared-laws marker without opposite-version control
  # shapes is classified; partial and mixed trees remain unknown.
  profile="$root/.re-discipline/project-profile.md"
  [ -f "$profile" ] || { printf 'unknown\n'; return; }
  legacy_profile=false
  current_profile=false
  grep -q 're-discipline:shared-laws v0\.7\.' "$profile" && legacy_profile=true
  grep -q 're-discipline:shared-laws v0\.8\.' "$profile" && current_profile=true
  legacy_shape=false
  current_shape=false
  [ -e "$root/.re-discipline/state" ] && current_shape=true
  [ -e "$root/docs/history/campaigns" ] && current_shape=true
  if [ -d "$root/active" ]; then
    for campaign in "$root"/active/*; do
      [ -d "$campaign" ] || continue
      if [ -f "$campaign/CAMPAIGN.md" ] || [ -f "$campaign/REVIEWS.md" ]; then legacy_shape=true; fi
      if [ -f "$campaign/campaign.json" ] || [ -d "$campaign/work-items" ] ||
         [ -d "$campaign/runs" ] || [ -d "$campaign/events" ] || [ -d "$campaign/closure" ]; then
        current_shape=true
      fi
    done
  fi
  if [ "$legacy_profile" = true ] && [ "$current_profile" = false ] && [ "$current_shape" = false ]; then
    printf '0.7\n'
    return
  fi
  if [ "$current_profile" = true ] && [ "$legacy_profile" = false ] && [ "$legacy_shape" = false ]; then
    printf '0.8\n'
    return
  fi
  printf 'unknown\n'
}

server_health() {
  root=$1
  plugin_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
  manifest="$plugin_root/knowledge/bin/manifest.json"
  plugin_manifest="$plugin_root/.codex-plugin/plugin.json"
  runtime="$plugin_root/knowledge/bin/re-discipline-knowledge"
  if [ ! -f "$manifest" ] || [ ! -f "$plugin_manifest" ] || [ ! -x "$runtime" ]; then printf 'runtime unavailable\n'; return; fi
  version=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$manifest" | head -n 1)
  plugin_version=$(sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$plugin_manifest" | head -n 1)
  semver='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
  if ! printf '%s\n' "$version" | grep -Eq "$semver"; then
    printf "runtime manifest version '%s' is invalid\n" "$version"
    return
  fi
  if ! printf '%s\n' "$plugin_version" | grep -Eq "$semver"; then
    printf "plugin manifest version '%s' is invalid\n" "$plugin_version"
    return
  fi
  # A local cachebuster is SemVer build metadata and does not change the
  # packaged runtime. Project-state version 0.8 is a separate contract.
  runtime_release=${version%%+*}
  plugin_release=${plugin_version%%+*}
  if [ "$runtime_release" != "$plugin_release" ]; then
    printf 'runtime %s does not match plugin %s\n' "$version" "$plugin_version"
    return
  fi
  if "$runtime" preflight --asset-root "$plugin_root/knowledge" --project-root "$root" >/dev/null 2>&1; then
    printf 'preflight passed (runtime %s)\n' "$version"
  else
    printf 'preflight needs attention (runtime %s)\n' "$version"
  fi
}

file_json_string() {
  file=$1
  key=$2
  tr -d '\r\n' < "$file" | sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" | head -n 1
}

find_registered_run() {
  root=$1
  run_id=$2
  case "$run_id" in R-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9]*) ;; *) return 1 ;; esac
  matches=
  count=0
  for candidate in "$root"/active/*/runs/"$run_id"/run.json; do
    [ -f "$candidate" ] || continue
    matches=$candidate
    count=$((count + 1))
  done
  [ "$count" -eq 1 ] || return 1
  [ "$(file_json_string "$matches" id)" = "$run_id" ] || return 1
  printf '%s\n' "$matches"
}

file_context_pack_path() {
  file=$1
  tr -d '\r\n' < "$file" |
    sed -n 's/.*"contextPack"[[:space:]]*:[[:space:]]*{[^}]*"path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n 1
}

validate_draft_run() {
  root=$1
  run_id=$2
  run_path=$3
  work_item=$4
  pack_digest=$5
  allow_returned=${6:-false}

  [ -n "$run_id" ] && [ -n "$run_path" ] || return 1
  printf '%s\n' "$pack_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || return 1
  [ -d "$run_path" ] || return 1
  rel=$(relative_path "$run_path" "$root")
  kind=$(printf '%s\n' "$rel" | awk -F/ '
    NF == 4 && $1 == "active" && $3 == "runs" { print "active"; exit }
    NF == 6 && $1 == ".re-discipline" && $2 == "agents" && $3 == "recruiting" && $5 == "runs" { print "recruiting"; exit }
  ')
  [ -n "$kind" ] || return 1
  [ "$(basename "${run_path%/}")" = "$run_id" ] || return 1
  case "$run_id" in [A-Za-z0-9]* ) ;; *) return 1 ;; esac
  case "$run_id" in *[!A-Za-z0-9._-]* ) return 1 ;; esac
  [ "${#run_id}" -ge 2 ] && [ "${#run_id}" -le 125 ] || return 1

  record="$run_path/run.json"
  [ -f "$record" ] || return 1
  [ "$(file_json_string "$record" id)" = "$run_id" ] || return 1
  status=$(file_json_string "$record" status)
  if [ "$allow_returned" = true ]; then
    case "$status" in prepared|running|returned) ;; *) return 1 ;; esac
  else
    case "$status" in prepared|running) ;; *) return 1 ;; esac
  fi

  if [ "$kind" = active ]; then
    [ -n "$work_item" ] || return 1
    [ "$(file_json_string "$record" primaryWorkItemId)" = "$work_item" ] || return 1
    registered=$(find_registered_run "$root" "$run_id") || return 1
    [ "$(relative_path "$(dirname "$registered")" "$root")" = "$rel" ] || return 1
    validated_work_item=$work_item
  else
    validated_work_item=none
  fi

  expected_pack="$rel/context-pack.json"
  [ "$(file_context_pack_path "$record")" = "$expected_pack" ] || return 1
  pack="$run_path/context-pack.json"
  [ -f "$pack" ] || return 1
  [ "$(file_json_string "$pack" digest)" = "$pack_digest" ] || return 1
  [ -n "$(file_json_string "$pack" packId)" ] || return 1
  validated_run_path=$(cd "$run_path" 2>/dev/null && pwd -P) || return 1
  return 0
}

subagent_id() {
  value=$(json_envelope_string agent_id); [ -n "$value" ] || value=$(json_envelope_string agentId)
  [ -n "$value" ] || value=$(json_object_string subagent agent_id 2>/dev/null || true)
  [ -n "$value" ] || value=$(json_object_string subagent agentId 2>/dev/null || true)
  [ -n "$value" ] || value=$(json_object_string subAgent agent_id 2>/dev/null || true)
  [ -n "$value" ] || value=$(json_object_string subAgent agentId 2>/dev/null || true)
  printf '%s\n' "$value"
}

safe_host_identifier() {
  value=$1
  [ -n "$value" ] && [ "${#value}" -le 200 ] || return 1
  case "$value" in [A-Za-z0-9]* ) ;; *) return 1 ;; esac
  case "$value" in *[!A-Za-z0-9._-]* ) return 1 ;; esac
  return 0
}

safe_dispatch_directory() {
  dispatch_root=$1
  dispatch_session=$2
  dispatch_create=${3:-false}
  safe_host_identifier "$dispatch_session" || return 1
  dispatch_current=$dispatch_root/.re-discipline
  [ -d "$dispatch_current" ] && [ ! -L "$dispatch_current" ] || return 1
  for dispatch_component in cache hook-dispatch v1 "$dispatch_session"; do
    dispatch_current=$dispatch_current/$dispatch_component
    if [ -e "$dispatch_current" ] || [ -L "$dispatch_current" ]; then
      [ -d "$dispatch_current" ] && [ ! -L "$dispatch_current" ] || return 1
    else
      [ "$dispatch_create" = true ] || return 1
      mkdir "$dispatch_current" 2>/dev/null || {
        [ -d "$dispatch_current" ] && [ ! -L "$dispatch_current" ] || return 1
      }
    fi
  done
  dispatch_session_dir=$dispatch_current
  return 0
}

first_session_start() {
  first_root=$1
  first_session=$2
  [ "$first_session" != unknown ] || return 0
  safe_dispatch_directory "$first_root" "$first_session" true || return 0
  first_marker=$dispatch_session_dir/session-start.emitted
  if (set -C; : > "$first_marker") 2>/dev/null; then
    return 0
  fi
  [ -f "$first_marker" ] && [ ! -L "$first_marker" ] && return 1
  return 0
}

dispatch_marker() {
  marker_present=false
  marker_valid=false
  marker_reason=
  marker_run_id=
  marker_pack_digest=
  marker_message=$(json_tool_string message 2>/dev/null || true)
  [ -n "$marker_message" ] || marker_message=$(json_tool_string prompt 2>/dev/null || true)
  [ -n "$marker_message" ] || return 0
  marker_first=$(printf '%s\n' "$marker_message" | sed -n '1p')
  case "$marker_first" in re-discipline-run:*) marker_present=true ;; *) return 0 ;; esac
  if ! printf '%s\n' "$marker_first" | grep -Eq '^re-discipline-run: R-[0-9]{8}-[0-9]{4,} sha256:[0-9a-f]{64}$'; then
    marker_reason='the first message line must be re-discipline-run: <R-id> <context-pack-digest>'
    return 0
  fi
  marker_run_id=$(printf '%s\n' "$marker_first" | awk '{print $2}')
  marker_pack_digest=$(printf '%s\n' "$marker_first" | awk '{print $3}')
  marker_valid=true
}

resolve_dispatch_draft_run() {
  dispatch_root=$1
  dispatch_run_id=$2
  dispatch_pack_digest=$3
  dispatch_allow_returned=${4:-false}
  dispatch_record=$(find_registered_run "$dispatch_root" "$dispatch_run_id") || return 1
  dispatch_run_path=$(dirname "$dispatch_record")
  dispatch_work_item=$(file_json_string "$dispatch_record" primaryWorkItemId)
  validate_draft_run "$dispatch_root" "$dispatch_run_id" "$dispatch_run_path" \
    "$dispatch_work_item" "$dispatch_pack_digest" "$dispatch_allow_returned"
}

ticket_field() {
  ticket_path=$1
  ticket_key=$2
  sed -n "s/^$ticket_key=//p" "$ticket_path"
}

read_dispatch_ticket() {
  ticket_path=$1
  ticket_expected_session=$2
  [ -f "$ticket_path" ] && [ ! -L "$ticket_path" ] || return 1
  ticket_size=$(wc -c < "$ticket_path" | tr -d '[:space:]')
  case "$ticket_size" in ''|*[!0-9]*) return 1 ;; esac
  [ "$ticket_size" -le 2048 ] || return 1
  ticket_lines=$(wc -l < "$ticket_path" | tr -d '[:space:]')
  ticket_schema=$(ticket_field "$ticket_path" schemaVersion)
  case "$ticket_schema" in
    1)
      [ "$ticket_lines" -eq 6 ] || return 1
      ticket_keys='schemaVersion sessionId runId contextPackDigest toolUseId createdUnix'
      ticket_kind=registered
      ;;
    2)
      [ "$ticket_lines" -eq 7 ] || return 1
      ticket_keys='schemaVersion sessionId kind runId contextPackDigest toolUseId createdUnix'
      ticket_kind=$(ticket_field "$ticket_path" kind)
      case "$ticket_kind" in ordinary|registered) ;; *) return 1 ;; esac
      ;;
    *) return 1 ;;
  esac
  for ticket_key in $ticket_keys; do
    [ "$(grep -c "^$ticket_key=" "$ticket_path")" -eq 1 ] || return 1
  done
  [ "$(ticket_field "$ticket_path" sessionId)" = "$ticket_expected_session" ] || return 1
  ticket_run_id=$(ticket_field "$ticket_path" runId)
  ticket_pack_digest=$(ticket_field "$ticket_path" contextPackDigest)
  ticket_tool_use_id=$(ticket_field "$ticket_path" toolUseId)
  ticket_created_unix=$(ticket_field "$ticket_path" createdUnix)
  case "$ticket_tool_use_id" in *[!A-Za-z0-9._:-]* ) return 1 ;; esac
  [ "${#ticket_tool_use_id}" -le 200 ] || return 1
  case "$ticket_created_unix" in ''|*[!0-9]*) return 1 ;; esac
  if [ "$ticket_kind" = registered ]; then
    printf '%s\n' "$ticket_run_id" | grep -Eq '^R-[0-9]{8}-[0-9]{4,}$' || return 1
    printf '%s\n' "$ticket_pack_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || return 1
  else
    [ -z "$ticket_run_id" ] && [ -z "$ticket_pack_digest" ] || return 1
  fi
  return 0
}

remove_stale_pending_dispatch() {
  stale_session_dir=$1
  stale_pending=$stale_session_dir/pending.ticket
  [ -f "$stale_pending" ] && [ ! -L "$stale_pending" ] || return 0
  stale_created=$(ticket_field "$stale_pending" createdUnix 2>/dev/null || true)
  stale_now=$(date +%s 2>/dev/null || printf '0')
  case "$stale_created:$stale_now" in *[!0-9:]*|:*) return 0 ;; esac
  if [ $((stale_now - stale_created)) -gt 30 ]; then rm -f -- "$stale_pending"; fi
}

reserve_dispatch_ticket() {
  reserve_root=$1
  reserve_session=$2
  reserve_kind=$3
  reserve_run_id=$4
  reserve_pack_digest=$5
  reserve_tool_use_id=$6
  reservation_reason=
  safe_host_identifier "$reserve_session" || {
    reservation_reason='host session_id is missing or unsafe'
    return 1
  }
  case "$reserve_tool_use_id" in *[!A-Za-z0-9._:-]* )
    reservation_reason='host tool_use_id is unsafe'
    return 1
  esac
  [ "${#reserve_tool_use_id}" -le 200 ] || {
    reservation_reason='host tool_use_id is unsafe'
    return 1
  }
  case "$reserve_kind" in
    registered)
      printf '%s\n' "$reserve_run_id" | grep -Eq '^R-[0-9]{8}-[0-9]{4,}$' || {
        reservation_reason='dispatch kind or run identity is invalid'
        return 1
      }
      printf '%s\n' "$reserve_pack_digest" | grep -Eq '^sha256:[0-9a-f]{64}$' || {
        reservation_reason='dispatch kind or run identity is invalid'
        return 1
      }
      ;;
    ordinary)
      if [ -n "$reserve_run_id" ] || [ -n "$reserve_pack_digest" ]; then
        reservation_reason='dispatch kind or run identity is invalid'
        return 1
      fi
      ;;
    *)
      reservation_reason='dispatch kind or run identity is invalid'
      return 1
      ;;
  esac
  safe_dispatch_directory "$reserve_root" "$reserve_session" true || {
    reservation_reason='dispatch cache path is unavailable or unsafe'
    return 1
  }
  reserve_pending=$dispatch_session_dir/pending.ticket
  reserve_started=$(date +%s)
  while :; do
    remove_stale_pending_dispatch "$dispatch_session_dir"
    reserve_created=$(date +%s)
    if (umask 077; set -C; {
      printf 'schemaVersion=2\n'
      printf 'sessionId=%s\n' "$reserve_session"
      printf 'kind=%s\n' "$reserve_kind"
      printf 'runId=%s\n' "$reserve_run_id"
      printf 'contextPackDigest=%s\n' "$reserve_pack_digest"
      printf 'toolUseId=%s\n' "$reserve_tool_use_id"
      printf 'createdUnix=%s\n' "$reserve_created"
    } > "$reserve_pending") 2>/dev/null; then
      return 0
    fi
    reserve_now=$(date +%s)
    if [ $((reserve_now - reserve_started)) -ge 4 ]; then
      reservation_reason='the prior launch handoff did not bind within the internal dispatch window'
      return 1
    fi
    sleep 0.05
  done
}

resolve_agent_dispatch() {
  resolve_root=$1
  resolve_session=$2
  resolve_agent=$3
  resolve_allow_returned=${4:-false}
  dispatch_found=false
  dispatch_valid=false
  dispatch_reason=
  dispatch_kind=
  dispatch_run_id=
  dispatch_pack_digest=
  safe_host_identifier "$resolve_session" || return 0
  safe_host_identifier "$resolve_agent" || return 0
  safe_dispatch_directory "$resolve_root" "$resolve_session" false || return 0
  resolve_binding=$dispatch_session_dir/agents/$resolve_agent.ticket
  [ -f "$resolve_binding" ] || return 0
  dispatch_found=true
  if ! read_dispatch_ticket "$resolve_binding" "$resolve_session"; then
    dispatch_reason='dispatch binding is invalid'
    return 0
  fi
  if [ "$ticket_kind" = ordinary ]; then
    dispatch_valid=true
    dispatch_kind=ordinary
    return 0
  fi
  if ! resolve_dispatch_draft_run "$resolve_root" "$ticket_run_id" "$ticket_pack_digest" "$resolve_allow_returned"; then
    dispatch_reason="bound run '$ticket_run_id' no longer matches its registered context pack"
    return 0
  fi
  dispatch_valid=true
  dispatch_kind=registered
  dispatch_run_id=$ticket_run_id
  dispatch_pack_digest=$ticket_pack_digest
}

claim_agent_dispatch() {
  claim_root=$1
  claim_session=$2
  claim_agent=$3
  dispatch_found=false
  dispatch_valid=false
  dispatch_reason=
  safe_host_identifier "$claim_session" || return 0
  safe_host_identifier "$claim_agent" || return 0
  safe_dispatch_directory "$claim_root" "$claim_session" false || return 0
  claim_session_dir=$dispatch_session_dir
  claim_agents=$claim_session_dir/agents
  if [ -e "$claim_agents" ] || [ -L "$claim_agents" ]; then
    if [ ! -d "$claim_agents" ] || [ -L "$claim_agents" ]; then
      dispatch_found=true
      dispatch_reason='agent binding directory is unavailable or unsafe'
      return 0
    fi
  else
    mkdir "$claim_agents" 2>/dev/null || {
      dispatch_found=true
      dispatch_reason='agent binding directory is unavailable or unsafe'
      return 0
    }
  fi
  claim_pending=$claim_session_dir/pending.ticket
  claim_binding=$claim_agents/$claim_agent.ticket
  if [ ! -f "$claim_binding" ]; then
    [ -f "$claim_pending" ] || return 0
    if ! mv "$claim_pending" "$claim_binding" 2>/dev/null; then
      dispatch_found=true
      dispatch_reason='pending dispatch could not be claimed atomically'
      return 0
    fi
  fi
  resolve_agent_dispatch "$claim_root" "$claim_session" "$claim_agent" false
}

clear_pending_dispatch_after_tool() {
  clear_root=$1
  clear_session=$2
  clear_tool_use_id=$3
  safe_dispatch_directory "$clear_root" "$clear_session" false || return 0
  clear_pending=$dispatch_session_dir/pending.ticket
  if read_dispatch_ticket "$clear_pending" "$clear_session" &&
     [ -n "$ticket_tool_use_id" ] && [ "$ticket_tool_use_id" = "$clear_tool_use_id" ]; then
    rm -f -- "$clear_pending"
  fi
}

valid_grant_path() {
  grant=$1
  case "$grant" in ''|/*|*\\*|*:*|*'"'*|*'['*|*']'*|*'{'*|*'}'*|*','*|*'*'*|*'?'*) return 1 ;; esac
  case "/$grant/" in */../*|*/./*|*//*) return 1 ;; esac
  return 0
}

registered_run_write_allowed() {
  root=$1
  run_id=$2
  rel=$3
  run_record=$(find_registered_run "$root" "$run_id") || return 1
  status=$(file_json_string "$run_record" status)
  case "$status" in prepared|running) ;; *) return 1 ;; esac
  run_dir=$(dirname "$run_record")
  run_rel=$(relative_path "$run_dir" "$root")
  case "$rel" in "$run_rel/report.md"|"$run_rel/payload/"*) return 0 ;; esac

  grant_json=$(tr -d '\r\n' < "$run_record" | sed -n 's/.*"writeGrants"[[:space:]]*:[[:space:]]*\[\([^]]*\)\].*/\1/p')
  [ -n "$grant_json" ] || return 1
  grant_lines=$(printf '%s' "$grant_json" | sed 's/},{/}\
{/g')
  old_ifs=$IFS
  IFS='
'
  for grant in $grant_lines; do
    grant_mode=$(printf '%s' "$grant" | sed -n 's/.*"mode"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
    grant_path=$(printf '%s' "$grant" | sed -n 's/.*"path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
    valid_grant_path "$grant_path" || { IFS=$old_ifs; return 1; }
    case "$grant_mode" in
      exact) [ "$rel" = "$grant_path" ] && { IFS=$old_ifs; return 0; } ;;
      directory) case "$rel" in "$grant_path"|"$grant_path/"*) IFS=$old_ifs; return 0 ;; esac ;;
      *) IFS=$old_ifs; return 1 ;;
    esac
  done
  IFS=$old_ifs
  return 1
}

cwd=$(json_string cwd)
[ -n "$cwd" ] || cwd=$(json_string projectRoot)
[ -n "$cwd" ] || cwd=$(pwd -P)
root=$(find_root "$cwd")

if [ "$event" = pre-tool-use ]; then
  tool=$(json_envelope_string tool_name)
  [ -n "$tool" ] || tool=$(json_envelope_string toolName)
  session_id=$(json_envelope_string session_id)
  [ -n "$session_id" ] || session_id=$(json_envelope_string sessionId)
  agent_id=$(subagent_id)
  tool_use_id=$(json_envelope_string tool_use_id)
  [ -n "$tool_use_id" ] || tool_use_id=$(json_envelope_string toolUseId)

  case "$tool" in
    spawn_agent|Agent)
      dispatch_marker
      if [ "$marker_present" != true ]; then
        if [ -z "$root" ]; then
          printf '{}\n'
        elif ! reserve_dispatch_ticket "$root" "$session_id" ordinary '' '' "$tool_use_id"; then
          emit_deny "Subagent launch could not enter the session dispatch boundary: $reservation_reason."
        else
          printf '{}\n'
        fi
        exit 0
      fi
      if [ "$marker_valid" != true ]; then
        emit_deny "Registered subagent launch denied: $marker_reason."
        exit 0
      fi
      if [ -z "$root" ] || [ "$(project_version "$root")" != 0.8 ]; then
        emit_deny 'Registered subagent launch denied: the current directory is not a verified re-discipline 0.8 project.'
        exit 0
      fi
      if ! resolve_dispatch_draft_run "$root" "$marker_run_id" "$marker_pack_digest" false; then
        emit_deny "Registered subagent launch denied: run '$marker_run_id' is not uniquely writable or does not match context pack '$marker_pack_digest'."
        exit 0
      fi
      if ! reserve_dispatch_ticket "$root" "$session_id" registered "$marker_run_id" "$marker_pack_digest" "$tool_use_id"; then
        emit_deny "Registered subagent launch denied: $reservation_reason."
        exit 0
      fi
      printf '{}\n'
      exit 0
      ;;
  esac

  [ -n "$root" ] || { printf '{}\n'; exit 0; }

  targets=
  operation_label=Write/Edit
  case "$tool" in
    Write|Edit)
      path=$(json_tool_string file_path 2>/dev/null || true)
      [ -n "$path" ] || path=$(json_tool_string filePath 2>/dev/null || true)
      [ -n "$path" ] || path=$(json_tool_string path 2>/dev/null || true)
      [ -n "$path" ] || path=$(json_envelope_string file_path)
      [ -n "$path" ] || path=$(json_envelope_string filePath)
      [ -n "$path" ] || path=$(json_envelope_string path)
      targets=$path
      ;;
    apply_patch)
      operation_label=apply_patch
      if ! command=$(json_tool_command); then
        emit_deny 'Direct apply_patch denied: patch command is missing or malformed. A write hook must identify every project-relative target before allowing the patch.'
        exit 0
      fi
      if ! raw_targets=$(patch_targets "$command"); then
        emit_deny 'Direct apply_patch denied: patch envelope or target list is malformed. A write hook must identify every project-relative target before allowing the patch.'
        exit 0
      fi
      targets=
      old_ifs=$IFS
      IFS='
'
      for raw_target in $raw_targets; do
        if ! normalized_target=$(valid_patch_target "$raw_target"); then
          IFS=$old_ifs
          emit_deny "Direct apply_patch denied: target '$raw_target' is not a supported filesystem path."
          exit 0
        fi
        if [ -n "$targets" ]; then targets="$targets
$normalized_target"; else targets=$normalized_target; fi
      done
      IFS=$old_ifs
      [ -n "$targets" ] || {
        emit_deny 'Direct apply_patch denied: no write target could be identified.'
        exit 0
      }
      ;;
    *) printf '{}\n'; exit 0 ;;
  esac

  [ -n "$targets" ] || { printf '{}\n'; exit 0; }
  resolve_agent_dispatch "$root" "$session_id" "$agent_id" false
  if [ "$dispatch_found" = true ] && [ "$dispatch_valid" != true ]; then
    emit_deny "Direct $operation_label denied: $dispatch_reason. The subagent has no usable registered run boundary."
    exit 0
  fi
  declared_run=$(json_envelope_string runId); [ -n "$declared_run" ] || declared_run=$(json_envelope_string run_id)
  if [ -z "$agent_id" ]; then
    [ -n "$declared_run" ] || declared_run=${RE_DISCIPLINE_RUN_ID:-}
  fi
  if [ -z "$declared_run" ] && [ -z "$agent_id" ]; then
    declared_path=$(json_envelope_string runPath); [ -n "$declared_path" ] || declared_path=$(json_envelope_string run_path)
    [ -n "$declared_path" ] || declared_path=${RE_DISCIPLINE_RUN_PATH:-}
    candidate_id=$(basename "${declared_path%/}")
    case "$candidate_id" in R-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9]*) declared_run=$candidate_id ;; esac
  fi
  if [ "$dispatch_valid" = true ]; then
    if [ "$dispatch_kind" = ordinary ]; then
      if [ -n "$declared_run" ]; then
        emit_deny "Direct $operation_label denied: an ordinary subagent launch cannot claim registered run '$declared_run'."
        exit 0
      fi
    else
      if [ -n "$declared_run" ] && [ "$declared_run" != "$dispatch_run_id" ]; then
        emit_deny "Direct $operation_label denied: host binding names run '$dispatch_run_id' but the tool envelope names '$declared_run'."
        exit 0
      fi
      declared_run=$dispatch_run_id
    fi
  elif [ -n "$agent_id" ] && [ -z "$declared_run" ]; then
    emit_deny "Direct $operation_label denied: subagent '$agent_id' has no registered run binding in manager session '$session_id'."
    exit 0
  fi
  project_state=$(project_version "$root")
  old_ifs=$IFS
  IFS='
'
  for target in $targets; do
    rel=$(relative_path "$target" "$root")
    if [ -z "$rel" ]; then
      if [ "$tool" = apply_patch ]; then
        IFS=$old_ifs
        emit_deny "Direct apply_patch denied: target '$target' is outside the verified project root or cannot be normalized."
        exit 0
      fi
      continue
    fi
    policy_rel=$(printf '%s' "$rel" | tr '[:upper:]' '[:lower:]')
    is_run_output=false
    case "$policy_rel" in active/*/runs/*/report.md|active/*/runs/*/payload/?*) is_run_output=true ;; esac
    if [ "$is_run_output" = true ] && [ -z "$declared_run" ]; then
      IFS=$old_ifs
      emit_deny "Direct $operation_label denied: run output '$rel' requires a uniquely registered writable run identity. Run grants are an accident boundary, not host-attested authority."
      exit 0
    fi
    if [ -n "$declared_run" ] && ! registered_run_write_allowed "$root" "$declared_run" "$rel"; then
      IFS=$old_ifs
      emit_deny "Direct $operation_label denied: path '$rel' is outside the uniquely registered writable run '$declared_run' and its project grants. Run grants are an accident boundary, not host-attested authority."
      exit 0
    fi
    if [ "$project_state" != 0.8 ] && legacy_canonical_path "$policy_rel"; then
      IFS=$old_ifs
      emit_deny "Direct $operation_label to '$rel' is blocked while the project is legacy or unverified. Use migrate-project for the approved conversion; no 0.8 operation may mutate prior-version state."
      exit 0
    fi
    if [ "$is_run_output" != true ] && protected_path "$policy_rel"; then
      IFS=$old_ifs
      emit_deny "Direct $operation_label to '$rel' is blocked. Use the re-discipline shared state engine; use migrate-project only for prior-version inputs."
      exit 0
    fi
  done
  IFS=$old_ifs
  printf '{}\n'
  exit 0
fi

if [ "$event" = post-tool-use ]; then
  tool=$(json_envelope_string tool_name)
  [ -n "$tool" ] || tool=$(json_envelope_string toolName)
  case "$tool" in
    spawn_agent|Agent)
      if [ -n "$root" ]; then
        session_id=$(json_envelope_string session_id)
        [ -n "$session_id" ] || session_id=$(json_envelope_string sessionId)
        tool_use_id=$(json_envelope_string tool_use_id)
        [ -n "$tool_use_id" ] || tool_use_id=$(json_envelope_string toolUseId)
        clear_pending_dispatch_after_tool "$root" "$session_id" "$tool_use_id"
      fi
      ;;
  esac
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
session_id=$(json_string session_id); [ -n "$session_id" ] || session_id=$(json_string sessionId)
[ -n "$session_id" ] || session_id=unknown

case "$event" in
  session-start)
    if ! first_session_start "$root" "$session_id"; then
      printf '{}\n'
      exit 0
    fi
    project_state=$(project_version "$root")
    if [ "$project_state" = 0.7 ]; then
      emit_context SessionStart 'Legacy re-discipline 0.7 project detected. Do not invoke 0.8 lifecycle operations or edit managed state directly. Inspect migrate-project status and create a read-only preview; migration requires explicit approval of the exact plan digest and never runs at session start.'
    elif [ "$project_state" != 0.8 ]; then
      emit_context SessionStart 'A re-discipline project marker was found, but its state version could not be verified. Do not invoke lifecycle mutations or edit managed state directly. Repair runtime availability or inspect migrate-project status before continuing.'
    else
      health=$(server_health "$root")
      handles=none
      if [ -d "$root/active" ]; then
        handles=$(for record in "$root"/active/*/campaign.json; do
          [ -f "$record" ] || continue
          basename "$(dirname "$record")"
        done | sort | head -n 8 | paste -sd ', ' -)
        [ -n "$handles" ] || handles=none
      fi
      emit_context SessionStart "Re-discipline 0.8 project detected; server $health. Session-start onboarding boundary=$session_id. If this session has not completed onboarding, invoke onboard and call bounded state mode orient once before substantive work. After the first successful orient, onboarding is satisfied for this session: do not re-invoke the onboard skill for ordinary user messages, tool rounds, or compaction. A PostCompact bounded-state refresh is not onboarding. Re-run onboarding only for a new or resumed host session, or after an explicit runtime/state invalidation. Active campaign handles: $handles. Canonical records are engine-owned; generated views and caches are derived."
    fi
    ;;
  pre-compact)
    emit_context PreCompact "No semantic save is required. Persist only an already-started atomic engine transaction. Resume handles: campaign=$campaign workItem=$work_item generation=$generation lastEvent=$event_head."
    ;;
  post-compact)
    emit_context PostCompact "Rehydrate with bounded state mode orient, then state mode resume for campaign=$campaign since generation=$generation or lastEvent=$event_head. Expand only cited handles needed for the next decision."
    ;;
  subagent-start)
    agent_id=$(subagent_id)
    claim_agent_dispatch "$root" "$session_id" "$agent_id"
    if [ "$dispatch_found" = true ]; then
      if [ "$dispatch_valid" != true ]; then
        emit_context SubagentStart "Registered re-discipline dispatch could not be bound for session=$session_id agent=$agent_id: $dispatch_reason. Do not write project or run files; return the binding failure to the manager."
      elif [ "$dispatch_kind" = ordinary ]; then
        printf '{}\n'
      else
        emit_context SubagentStart "Assigned session=$session_id agent=$agent_id run=$dispatch_run_id workItem=$validated_work_item path=$validated_run_path contextPackDigest=$dispatch_pack_digest. Read the exact brief and verify the immutable pack. Write only report.md, lazy payload/, and explicit project grants. Do not mutate canonical state or ratify findings."
      fi
    else
      declared_run_id=$(json_string runId); [ -n "$declared_run_id" ] || declared_run_id=$(json_string run_id)
      declared_run_path=$(json_string runPath); [ -n "$declared_run_path" ] || declared_run_path=$(json_string run_path)
      declared_work_item=$(json_string workItemId); [ -n "$declared_work_item" ] || declared_work_item=$(json_string work_item_id)
      declared_pack_digest=$(json_string contextPackDigest); [ -n "$declared_pack_digest" ] || declared_pack_digest=$(json_string context_pack_digest)
      if validate_draft_run "$root" "$declared_run_id" "$declared_run_path" "$declared_work_item" "$declared_pack_digest" false; then
        emit_context SubagentStart "Assigned run=$declared_run_id workItem=$validated_work_item path=$validated_run_path contextPackDigest=$declared_pack_digest. Read the exact brief and verify the immutable pack. Write only report.md, lazy payload/, and explicit project grants. Do not mutate canonical state or ratify findings."
      else
        printf '{}\n'
      fi
    fi
    ;;
  subagent-stop)
    agent_id=$(subagent_id)
    resolve_agent_dispatch "$root" "$session_id" "$agent_id" true
    if [ "$dispatch_found" = true ] && [ "$dispatch_valid" != true ]; then
      emit_context SubagentStop "Registered re-discipline dispatch binding is invalid for session=$session_id agent=$agent_id: $dispatch_reason. The manager must repair or invalidate the run through the shared engine."
    elif [ "$dispatch_valid" = true ] && [ "$dispatch_kind" = ordinary ]; then
      printf '{}\n'
    else
      if [ "$dispatch_valid" = true ]; then
        declared_run_id=$dispatch_run_id
        draft_valid=true
      else
        declared_run_id=$(json_string runId); [ -n "$declared_run_id" ] || declared_run_id=$(json_string run_id)
        declared_run_path=$(json_string runPath); [ -n "$declared_run_path" ] || declared_run_path=$(json_string run_path)
        declared_work_item=$(json_string workItemId); [ -n "$declared_work_item" ] || declared_work_item=$(json_string work_item_id)
        declared_pack_digest=$(json_string contextPackDigest); [ -n "$declared_pack_digest" ] || declared_pack_digest=$(json_string context_pack_digest)
        if validate_draft_run "$root" "$declared_run_id" "$declared_run_path" "$declared_work_item" "$declared_pack_digest" true; then draft_valid=true; else draft_valid=false; fi
      fi
      if [ "$draft_valid" = true ]; then
      if [ -f "$validated_run_path/report.md" ]; then report_status='report present'; else report_status='report missing'; fi
      emit_context SubagentStop "Run return check for $declared_run_id: $report_status. Submit run.return through the shared engine to freeze the report digest and queue curation. Return does not imply review or ratification."
      else
        printf '{}\n'
      fi
    fi
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
