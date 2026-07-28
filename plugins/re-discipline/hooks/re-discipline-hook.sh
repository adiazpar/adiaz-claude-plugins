#!/bin/sh

event_name="${1:-}"
raw_input="$(cat)"
project_dir="$(printf '%s' "$raw_input" | sed -n 's/.*"cwd"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
source_name="$(printf '%s' "$raw_input" | sed -n 's/.*"source"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"

if [ -z "$project_dir" ]; then
  project_dir="${CLAUDE_PROJECT_DIR:-$(pwd)}"
fi

plugin_root="${PLUGIN_ROOT:-${CLAUDE_PLUGIN_ROOT:-}}"
is_codex=false
if [ -n "${PLUGIN_ROOT:-}" ]; then
  is_codex=true
fi

find_project_root() {
  current="$1"
  while [ -n "$current" ]; do
    if [ -f "$current/.re-discipline/project-profile.md" ] || \
       [ -f "$current/.re-discipline/config.json" ] || \
       [ -f "$current/.claude/project-profile.md" ] || \
       [ -f "$current/.codex/project-profile.md" ]; then
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

write_hook_context_text() {
  hook_event="$1"
  context="$2"
  printf '%s' '{"hookSpecificOutput":{"hookEventName":'
  json_string_from_text "$hook_event"
  printf '%s' ',"additionalContext":'
  json_string_from_text "$context"
  printf '%s\n' '}}'
}

write_hook_context_file() {
  hook_event="$1"
  context_file="$2"
  suffix="${3:-}"
  printf '%s' '{"hookSpecificOutput":{"hookEventName":'
  json_string_from_text "$hook_event"
  printf '%s' ',"additionalContext":'
  if [ -z "$suffix" ]; then
    json_string_from_file "$context_file"
  else
    context="$(cat "$context_file")"
    context="${context}

${suffix}"
    json_string_from_text "$context"
  fi
  printf '%s\n' '}}'
}

managed_path_is_safe() {
  safe_root="$1"
  safe_target="$2"
  case "$safe_target" in
    "$safe_root"/*) ;;
    *) return 1 ;;
  esac

  safe_relative="${safe_target#"$safe_root"/}"
  safe_current="$safe_root"
  safe_old_ifs="$IFS"
  IFS='/'
  for safe_component in $safe_relative; do
    [ -n "$safe_component" ] || continue
    safe_current="$safe_current/$safe_component"
    if [ -L "$safe_current" ]; then
      IFS="$safe_old_ifs"
      return 1
    fi
  done
  IFS="$safe_old_ifs"
  return 0
}

append_recovered() {
  if [ -z "$recovered_files" ]; then
    recovered_files="$1"
  else
    recovered_files="$recovered_files, $1"
  fi
}

append_warning() {
  if [ -z "$recovery_warnings" ]; then
    recovery_warnings="$1"
  else
    recovery_warnings="$recovery_warnings; $1"
  fi
}

atomic_install_temp() {
  atomic_root="$1"
  atomic_target="$2"
  atomic_temporary="$3"
  if ! managed_path_is_safe "$atomic_root" "$atomic_target"; then
    rm -f "$atomic_temporary"
    return 1
  fi
  if [ -e "$atomic_target" ]; then
    rm -f "$atomic_temporary"
    return 0
  fi
  mv "$atomic_temporary" "$atomic_target"
}

restore_managed_file() {
  root="$1"
  relative="$2"
  template_name="$3"
  target="$root/$relative"
  if [ -f "$target" ]; then
    return 0
  fi
  if [ -e "$target" ]; then
    append_warning "$relative exists but is not a regular file"
    return 0
  fi
  if ! managed_path_is_safe "$root" "$target"; then
    append_warning "$relative escapes through a link or outside the project"
    return 0
  fi

  parent="$(dirname "$target")"
  if ! managed_path_is_safe "$root" "$parent"; then
    append_warning "$relative has an unsafe parent"
    return 0
  fi
  mkdir -p "$parent" || {
    append_warning "$relative parent could not be created"
    return 0
  }
  temporary="$(mktemp "$parent/.re-discipline-recovery.XXXXXX")" || {
    append_warning "$relative temporary file could not be created"
    return 0
  }

  git_path="$(printf '%s' "$relative" | tr '\\' '/')"
  if command -v git >/dev/null 2>&1 && \
     git -C "$root" show "HEAD:$git_path" >"$temporary" 2>/dev/null; then
    :
  elif [ -n "$plugin_root" ] && [ -f "$plugin_root/templates/project/$template_name" ]; then
    cp "$plugin_root/templates/project/$template_name" "$temporary"
  else
    rm -f "$temporary"
    append_warning "$relative is missing and its packaged template is unavailable"
    return 0
  fi

  if atomic_install_temp "$root" "$target" "$temporary"; then
    if [ -f "$target" ]; then
      append_recovered "$relative"
    fi
  else
    append_warning "$relative was not recovered safely"
  fi
}

restore_host_file() {
  root="$1"
  relative="$2"
  enabled="$3"
  target="$root/$relative"
  if [ -f "$target" ]; then
    return 0
  fi
  if [ -e "$target" ]; then
    append_warning "$relative exists but is not a regular file"
    return 0
  fi
  if ! managed_path_is_safe "$root" "$target"; then
    append_warning "$relative escapes through a link or outside the project"
    return 0
  fi

  parent="$(dirname "$target")"
  mkdir -p "$parent" || {
    append_warning "$relative parent could not be created"
    return 0
  }
  temporary="$(mktemp "$parent/.re-discipline-recovery.XXXXXX")" || {
    append_warning "$relative temporary file could not be created"
    return 0
  }
  git_path="$(printf '%s' "$relative" | tr '\\' '/')"
  if command -v git >/dev/null 2>&1 && \
     git -C "$root" show "HEAD:$git_path" >"$temporary" 2>/dev/null; then
    :
  elif [ "$relative" = ".claude/settings.json" ]; then
    printf '{\n  "autoMemoryEnabled": %s\n}\n' "$enabled" >"$temporary"
  else
    {
      printf '%s\n' '# re-discipline:codex-memory-policy v1'
      printf '%s\n' '# Preserve unrelated project settings when changing this managed policy.'
      printf '%s\n' '[features]'
      printf 'memories = %s\n\n' "$enabled"
      printf '%s\n' '[memories]'
      printf 'generate_memories = %s\n' "$enabled"
      printf 'use_memories = %s\n' "$enabled"
      printf '%s\n' '# re-discipline:codex-memory-policy:end'
    } >"$temporary"
  fi

  if atomic_install_temp "$root" "$target" "$temporary"; then
    if [ -f "$target" ]; then
      append_recovered "$relative"
    fi
  else
    append_warning "$relative was not recovered safely"
  fi
}

extract_json_keys() {
  awk '
    BEGIN { in_string = 0; escaped = 0; value = "" }
    {
      text = $0
      length_text = length(text)
      for (cursor = 1; cursor <= length_text; cursor++) {
        character = substr(text, cursor, 1)
        if (in_string) {
          if (escaped) {
            escaped = 0
            value = value character
          } else if (character == "\\") {
            escaped = 1
          } else if (character == "\"") {
            lookahead = cursor + 1
            while (lookahead <= length_text && substr(text, lookahead, 1) ~ /[[:space:]]/) {
              lookahead++
            }
            if (substr(text, lookahead, 1) == ":") {
              print value
            }
            in_string = 0
            value = ""
          } else {
            value = value character
          }
        } else if (character == "\"") {
          in_string = 1
          value = ""
        }
      }
    }
  ' "$1"
}

validate_json_syntax() {
  path="$1"
  comments="${2:-0}"
  validator="$plugin_root/hooks/validate-json.awk"
  if [ -z "$plugin_root" ] || [ ! -f "$validator" ]; then
    return 1
  fi
  awk -v allow_comments="$comments" -f "$validator" "$path" >/dev/null 2>&1
}

validate_bootstrap() {
  config_path="$1"
  validate_json_syntax "$config_path" 0 || return 1
  expected_keys='enabled
knowledge
memory
mode
profile
projectProfile
schemaVersion
settingsDirectory
settingsFile
writePolicy'
  actual_keys="$(extract_json_keys "$config_path" | LC_ALL=C sort)"
  if [ "$actual_keys" != "$expected_keys" ]; then
    return 1
  fi
  compact="$(tr -d '\r\n\t ' <"$config_path")"
  printf '%s' "$compact" | grep -Eq '"schemaVersion":1' || return 1
  printf '%s' "$compact" | grep -Eq '"settingsDirectory":"settings"' || return 1
  printf '%s' "$compact" | grep -Eq '"mode":"(shared-only|hybrid|native)"' || return 1
  printf '%s' "$compact" | grep -Eq '"writePolicy":"proposal-only"' || return 1
  printf '%s' "$compact" | grep -Eq '"enabled":(true|false)' || return 1
  printf '%s' "$compact" | grep -Eq '"profile":"plugin:balanced-v1"' || return 1
  printf '%s' "$compact" | grep -Eq '"settingsFile":"settings/knowledge.jsonc"' || return 1
  printf '%s' "$compact" | grep -Eq '"projectProfile":"settings/retrieval-profile.json"' || return 1
  memory_mode="$(printf '%s' "$compact" | sed -n 's/.*"mode":"\([^"]*\)".*/\1/p')"
  [ -n "$memory_mode" ]
}

# Shared TOML structure scanner. This mirrors tomlCodeLines, openTOMLMultiline,
# tomlBracketDelta, stripTOMLComment, and tomlKeySegments in the packaged Go
# recovery scanner so the hook and the server agree on which lines carry TOML
# structure and on what a dotted key names. Interior lines of a multi-line
# string, array, or inline table are value content and are never tables,
# assignments, or comments.
#
# toml_key_path accepts bare, "basic"-quoted and literal-quoted key segments and
# returns their canonical dotted form, or "" when the key is malformed. Quoting
# is the only way to write a Windows path as a key, so a scanner that accepts
# only bare keys reports the section Codex itself writes - [projects."C:\Users\x"]
# - as malformed, and a host judged malformed never receives its memory policy.
toml_scan_awk='
function toml_trim(value) {
  gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
  return value
}

function toml_strip_comment(line,   position, span, character, basic, literal, escaped) {
  basic = 0
  literal = 0
  escaped = 0
  span = length(line)
  for (position = 1; position <= span; position++) {
    character = substr(line, position, 1)
    if (escaped) {
      escaped = 0
    } else if (basic && character == "\\") {
      escaped = 1
    } else if (!literal && character == "\"") {
      basic = !basic
    } else if (!basic && character == "\047") {
      literal = !literal
    } else if (!basic && !literal && character == "#") {
      return substr(line, 1, position - 1)
    }
  }
  return line
}

function toml_bracket_delta(text,   position, span, character, basic, literal, escaped, delta) {
  delta = 0
  basic = 0
  literal = 0
  escaped = 0
  span = length(text)
  for (position = 1; position <= span; position++) {
    character = substr(text, position, 1)
    if (escaped) {
      escaped = 0
    } else if (basic && character == "\\") {
      escaped = 1
    } else if (!literal && character == "\"") {
      basic = !basic
    } else if (!basic && character == "\047") {
      literal = !literal
    } else if (!basic && !literal && (character == "[" || character == "{")) {
      delta++
    } else if (!basic && !literal && (character == "]" || character == "}")) {
      delta--
    }
  }
  return delta
}

function toml_open_multiline(value) {
  if (substr(value, 1, 3) == "\"\"\"" &&
      index(substr(value, 4), "\"\"\"") == 0) {
    return "\"\"\""
  }
  if (substr(value, 1, 3) == "\047\047\047" &&
      index(substr(value, 4), "\047\047\047") == 0) {
    return "\047\047\047"
  }
  return ""
}

function toml_is_code(raw,   at, rest, line, equals, value, opened) {
  if (toml_error) {
    return 0
  }
  if (toml_delim != "") {
    at = index(raw, toml_delim)
    if (at == 0) {
      return 0
    }
    rest = substr(raw, at + length(toml_delim))
    toml_delim = ""
    toml_depth += toml_bracket_delta(toml_strip_comment(rest))
    if (toml_depth < 0) {
      toml_error = 1
    }
    return 0
  }
  if (toml_depth > 0) {
    toml_depth += toml_bracket_delta(toml_strip_comment(raw))
    if (toml_depth < 0) {
      toml_error = 1
    }
    return 0
  }
  line = toml_trim(toml_strip_comment(raw))
  equals = index(line, "=")
  if (equals > 1) {
    value = toml_trim(substr(line, equals + 1))
    opened = toml_open_multiline(value)
    if (opened != "") {
      toml_delim = opened
      return 1
    }
    toml_depth += toml_bracket_delta(value)
    if (toml_depth < 0) {
      toml_error = 1
    }
  }
  return 1
}

function toml_key_path(key,   rest, out, count, quote, segment, character, position, span, closed) {
  rest = key
  out = ""
  count = 0
  while (1) {
    sub(/^[[:space:]]+/, "", rest)
    if (rest == "") {
      return ""
    }
    quote = substr(rest, 1, 1)
    if (quote == "\"" || quote == "\047") {
      segment = ""
      closed = 0
      span = length(rest)
      position = 2
      while (position <= span) {
        character = substr(rest, position, 1)
        if (quote == "\"" && character == "\\") {
          if (position + 1 > span) {
            return ""
          }
          segment = segment substr(rest, position + 1, 1)
          position += 2
          continue
        }
        if (character == quote) {
          closed = 1
          position++
          break
        }
        segment = segment character
        position++
      }
      if (!closed) {
        return ""
      }
      rest = substr(rest, position)
    } else {
      segment = ""
      span = length(rest)
      position = 1
      while (position <= span) {
        character = substr(rest, position, 1)
        if (character == "." || character ~ /[[:space:]]/) {
          break
        }
        segment = segment character
        position++
      }
      if (segment !~ /^[A-Za-z0-9_-]+$/) {
        return ""
      }
      rest = substr(rest, position)
    }
    if (segment == "") {
      return ""
    }
    count++
    if (count == 1) {
      out = segment
    } else {
      out = out "." segment
    }
    sub(/^[[:space:]]+/, "", rest)
    if (rest == "") {
      return out
    }
    if (substr(rest, 1, 1) != ".") {
      return ""
    }
    rest = substr(rest, 2)
  }
}

function toml_continues() {
  return (toml_delim != "" || toml_depth > 0)
}

function toml_balanced() {
  return (!toml_error && toml_delim == "" && toml_depth == 0)
}
'

toml_boolean() {
  path="$1"
  target_section="$2"
  target_key="$3"
  awk -v wanted_section="$target_section" -v wanted_key="$target_key" \
    "$toml_scan_awk"'
    BEGIN { section = ""; count = 0; value = "" }
    {
      if (!toml_is_code($0)) {
        next
      }
      line = toml_trim(toml_strip_comment($0))
      if (substr(line, 1, 1) == "[" &&
          substr(line, length(line), 1) == "]" &&
          toml_key_path(substr(line, 2, length(line) - 2)) != "") {
        section = toml_key_path(substr(line, 2, length(line) - 2))
      } else if (section == wanted_section) {
        expression = "^" wanted_key "[[:space:]]*=[[:space:]]*(true|false)$"
        if (line ~ expression) {
          split(line, parts, "=")
          candidate = parts[2]
          gsub(/[[:space:]]/, "", candidate)
          count++
          value = candidate
        }
      }
    }
    END {
      if (count == 1 && toml_balanced()) {
        print value
      }
    }
  ' "$path"
}

validate_toml_policy_container() {
  awk "$toml_scan_awk"'
    function managed_path(value) {
      return value ~ /^(features|memories)(\.|$)/
    }
    BEGIN { active = "" }
    {
      if (!toml_is_code($0)) {
        if (toml_error) {
          exit 1
        }
        next
      }
      continued = toml_continues()
      line = toml_trim(toml_strip_comment($0))
      if (line == "") {
        next
      }
      if (substr(line, 1, 2) == "[[") {
        exit 1
      }
      if (substr(line, 1, 1) == "[") {
        if (substr(line, length(line), 1) != "]") {
          exit 1
        }
        raw_table = toml_trim(substr(line, 2, length(line) - 2))
        table = toml_key_path(raw_table)
        if (table == "") {
          exit 1
        }
        # A managed table must be spelled bare. The setter matches the managed
        # keys inside it by their literal text, so accepting an alternate
        # spelling here would let it append a second copy of a key that is
        # already present.
        if (managed_path(table) && raw_table != "features" &&
            raw_table != "memories") {
          exit 1
        }
        active = table
        next
      }
      equals = index(line, "=")
      if (equals > 1) {
        raw_left = toml_trim(substr(line, 1, equals - 1))
        left = toml_key_path(raw_left)
        if (left == "") {
          exit 1
        }
        right = toml_trim(substr(line, equals + 1))
        multiline = (continued ||
                     substr(right, 1, 3) == "\"\"\"" ||
                     substr(right, 1, 3) == "\047\047\047")
        if (!multiline &&
            right !~ /^"([^"\\]|\\.)*"$/ &&
            right !~ "^\047[^\047]*\047$" &&
            right !~ /^\[[^][]*\]$/ &&
            right !~ /^\{[^{}]*\}$/ &&
            right !~ /^(true|false|[+-]?(inf|nan)|[+-]?[0-9][0-9A-Za-z_.:+-]*)$/) {
          exit 1
        }
        if (active == "" && managed_path(left)) {
          exit 1
        }
        if (active == "features" &&
            left ~ /^memories(\.|$)/ &&
            raw_left != "memories") {
          exit 1
        }
        if (active == "memories" &&
            left ~ /^(generate_memories|use_memories)(\.|$)/ &&
            raw_left !~ /^(generate_memories|use_memories)$/) {
          exit 1
        }
        next
      }
      exit 1
    }
    END {
      if (!toml_balanced()) {
        exit 1
      }
    }
  ' "$1"
}

repair_host_policy() {
  root="$1"
  expected="$2"
  claude_path="$root/.claude/settings.json"
  json_setter="$plugin_root/hooks/set-json-memory.awk"
  if validate_json_syntax "$claude_path" 0 && [ -f "$json_setter" ]; then
    temporary="$(mktemp "$(dirname "$claude_path")/.re-discipline-policy.XXXXXX")" || {
      append_warning ".claude/settings.json temporary policy file could not be created"
      temporary=""
    }
    if [ -n "$temporary" ]; then
      if awk -v expected="$expected" -f "$json_setter" "$claude_path" >"$temporary" &&
         validate_json_syntax "$temporary" 0; then
        if ! cmp -s "$temporary" "$claude_path"; then
          if managed_path_is_safe "$root" "$claude_path" &&
             mv "$temporary" "$claude_path"; then
            append_recovered ".claude/settings.json (memory policy)"
          else
            rm -f "$temporary"
            append_warning ".claude/settings.json memory policy was not repaired safely"
          fi
        else
          rm -f "$temporary"
        fi
      else
        rm -f "$temporary"
        append_warning ".claude/settings.json has an ambiguous memory field and was preserved"
      fi
    fi
  else
    append_warning ".claude/settings.json is malformed and was preserved"
  fi

  codex_path="$root/.codex/config.toml"
  toml_setter="$plugin_root/hooks/set-toml-memory.awk"
  if ! validate_toml_policy_container "$codex_path"; then
    append_warning ".codex/config.toml is malformed or uses unsupported syntax and was preserved"
    return 0
  fi
  if [ ! -f "$toml_setter" ]; then
    append_warning ".codex/config.toml policy helper is unavailable"
    return 0
  fi
  toml_setter_program="$toml_scan_awk
$(cat "$toml_setter")"
  working="$(mktemp "$(dirname "$codex_path")/.re-discipline-policy.XXXXXX")" || {
    append_warning ".codex/config.toml temporary policy file could not be created"
    return 0
  }
  cp "$codex_path" "$working"
  valid=true
  for requirement in \
    "features memories" \
    "memories generate_memories" \
    "memories use_memories"; do
    section="${requirement%% *}"
    key="${requirement#* }"
    next="$(mktemp "$(dirname "$codex_path")/.re-discipline-policy.XXXXXX")" || {
      valid=false
      break
    }
    if awk \
      -v wanted_section="$section" \
      -v wanted_key="$key" \
      -v expected="$expected" \
      "$toml_setter_program" \
      "$working" >"$next"; then
      mv "$next" "$working"
    else
      rm -f "$next"
      valid=false
      break
    fi
  done
  if [ "$valid" != true ]; then
    rm -f "$working"
    append_warning ".codex/config.toml is malformed or ambiguous and was preserved"
    return 0
  fi
  if cmp -s "$working" "$codex_path"; then
    rm -f "$working"
  elif managed_path_is_safe "$root" "$codex_path" &&
       mv "$working" "$codex_path"; then
    append_recovered ".codex/config.toml (memory policy)"
  else
    rm -f "$working"
    append_warning ".codex/config.toml memory policy was not repaired safely"
  fi
}

validate_host_policy() {
  root="$1"
  expected="$2"
  claude_path="$root/.claude/settings.json"
  claude_valid=true
  if ! validate_json_syntax "$claude_path" 0; then
    append_warning ".claude/settings.json is malformed and was preserved"
    claude_valid=false
  fi
  compact="$(tr -d '\r\n\t ' <"$claude_path" 2>/dev/null)"
  if [ "$claude_valid" = true ] && \
     ! printf '%s' "$compact" | grep -Eq "\"autoMemoryEnabled\":$expected([,}])"; then
    append_warning ".claude/settings.json does not match memory mode '$memory_mode'"
  fi

  codex_path="$root/.codex/config.toml"
  for requirement in \
    "features memories" \
    "memories generate_memories" \
    "memories use_memories"; do
    section="${requirement%% *}"
    key="${requirement#* }"
    value="$(toml_boolean "$codex_path" "$section" "$key")"
    if [ "$value" != "$expected" ]; then
      append_warning ".codex/config.toml $section.$key does not match memory mode '$memory_mode'"
    fi
  done
}

get_profile_state() {
  profile_path="$1"
  profile_version="$(sed -n 's/.*re-discipline:shared-laws v\([0-9][0-9.]*\).*/\1/p' "$profile_path" 2>/dev/null | head -n 1)"
  case "$profile_version" in
    0.6.*) printf '%s\n' managed ;;
    "")
      printf '%s\n' legacy
      ;;
    0.[0-5].*) printf '%s\n' legacy ;;
    *) printf '%s\n' newer ;;
  esac
}

configuration_recovery() {
  root="$1"
  profile_state="$2"
  if [ "$profile_state" = newer ]; then
    append_warning "managed profile v$profile_version is newer than this hook; no recovery was attempted"
    return 0
  fi
  if [ "$profile_state" != managed ]; then
    return 0
  fi

  restore_managed_file "$root" ".re-discipline/config.json" "config.json"
  config_path="$root/.re-discipline/config.json"
  if [ ! -f "$config_path" ]; then
    return 0
  fi
  if ! validate_bootstrap "$config_path"; then
    append_warning "config.json is malformed or unsupported"
    return 0
  fi

  restore_managed_file "$root" ".re-discipline/settings/README.md" "settings-README.md"
  restore_managed_file "$root" ".re-discipline/settings/knowledge.jsonc" "knowledge.jsonc"
  restore_managed_file "$root" ".re-discipline/settings/retrieval-profile.json" "retrieval-profile.json"
  restore_managed_file "$root" ".re-discipline/memory/INDEX.md" "memory-INDEX.md"
  restore_managed_file "$root" ".re-discipline/knowledge/evals/README.md" "knowledge-evals-README.md"

  enabled=true
  if [ "$memory_mode" = shared-only ]; then
    enabled=false
  fi
  restore_host_file "$root" ".claude/settings.json" "$enabled"
  restore_host_file "$root" ".codex/config.toml" "$enabled"

  for relative_directory in \
    ".re-discipline/memory/proposals" \
    ".re-discipline/memory/topics" \
    ".re-discipline/knowledge/evals"; do
    directory="$root/$relative_directory"
    if managed_path_is_safe "$root" "$directory"; then
      mkdir -p "$directory" || append_warning "$relative_directory could not be created"
    else
      append_warning "$relative_directory escapes through a link"
    fi
  done

  knowledge_path="$root/.re-discipline/settings/knowledge.jsonc"
  if ! validate_json_syntax "$knowledge_path" 1; then
    append_warning "settings/knowledge.jsonc is malformed or unsupported"
  else
    knowledge_compact="$(tr -d '\r\n\t ' <"$knowledge_path" 2>/dev/null)"
    printf '%s' "$knowledge_compact" | grep -Eq '"schemaVersion":1' ||
      append_warning "settings/knowledge.jsonc is malformed or unsupported"
    printf '%s' "$knowledge_compact" | grep -Eq '"execution":"local"' ||
      append_warning "settings/knowledge.jsonc must keep model execution local"
  fi

  retrieval_path="$root/.re-discipline/settings/retrieval-profile.json"
  if ! validate_json_syntax "$retrieval_path" 0; then
    append_warning "settings/retrieval-profile.json is malformed or unsupported"
  else
    retrieval_compact="$(tr -d '\r\n\t ' <"$retrieval_path" 2>/dev/null)"
    printf '%s' "$retrieval_compact" | grep -Eq '"schemaVersion":1' ||
      append_warning "settings/retrieval-profile.json is malformed or unsupported"
    # Mirror profileIdentityRE in the packaged server. A project that has ever
    # calibrated and promoted a candidate carries "project:candidate-<hex>",
    # not the packaged baseline identity.
    printf '%s' "$retrieval_compact" |
      grep -Eq '"profileId":"[a-z0-9]+(-[a-z0-9]+)*(:[a-z0-9]+(-[a-z0-9]+)*)?"' ||
      append_warning "settings/retrieval-profile.json has an unsupported profile"
  fi

  repair_host_policy "$root" "$enabled"
  validate_host_policy "$root" "$enabled"
}

recovery_summary() {
  summary=""
  if [ -n "$recovered_files" ]; then
    summary="Recovered managed project files: $recovered_files"
  fi
  if [ -n "$recovery_warnings" ]; then
    warning_text="Re-discipline is in read-only degraded configuration state: $recovery_warnings. Invoke init-project repair; existing malformed files were not replaced."
    if [ -n "$summary" ]; then
      summary="${summary}
${warning_text}"
    else
      summary="$warning_text"
    fi
  fi
  printf '%s' "$summary"
}

knowledge_health_summary() {
  root="$1"
  executable="$plugin_root/knowledge/bin/re-discipline-knowledge"
  asset_root="$plugin_root/knowledge"

  if [ -z "$plugin_root" ]; then
    printf '%s' 'Knowledge health status is unavailable because the plugin root is missing.'
    return 0
  fi
  if [ ! -f "$executable" ] || [ ! -x "$executable" ] || [ ! -d "$asset_root" ]; then
    printf '%s' 'Knowledge health status is unavailable because the packaged runtime is missing.'
    return 0
  fi

  health_output="$("$executable" status \
    --asset-root "$asset_root" \
    --project-root "$root" 2>/dev/null)"
  health_status=$?
  if [ "$health_status" -ne 0 ]; then
    printf '%s' 'Knowledge health status reported degraded state; run the packaged status command directly.'
    return 0
  fi

  # Only ACTIONABLE benchmark staleness is surfaced at session start. Corpus
  # and evaluation-set drift are informational: documents change every day in a
  # living project, and reporting that as a warning teaches sessions to ignore
  # the warning that matters. A model, runtime, or chunker change is different,
  # because the measured behavior itself may no longer hold.
  health_compact="$(printf '%s' "$health_output" | tr -d ' \n\t\r')"
  case "$health_compact" in
    *'"staleActionable":true'*)
      health_reasons="$(printf '%s' "$health_compact" |
        sed -n 's/.*"actionableStaleReasons":\[\([^]]*\)\].*/\1/p' | tr -d '"')"
      if [ -n "$health_reasons" ]; then
        printf 'Knowledge benchmark evidence is actionably stale (%s); re-run benchmark-knowledge before relying on measured retrieval behavior.' \
          "$health_reasons"
      else
        printf '%s' 'Knowledge benchmark evidence is actionably stale; re-run benchmark-knowledge before relying on measured retrieval behavior.'
      fi
      ;;
  esac
}

active_campaign_reminder() {
  root="$1"
  [ -d "$root/active" ] || return 0
  handles=""
  count=0
  for campaign in "$root"/active/*/CAMPAIGN.md; do
    [ -f "$campaign" ] || continue
    relative="${campaign#"$root"/}"
    if [ -z "$handles" ]; then
      handles="$relative"
    else
      handles="$handles, $relative"
    fi
    count=$((count + 1))
    [ "$count" -ge 5 ] && break
  done
  if [ -n "$handles" ]; then
    printf ' Active campaign handles: %s.' "$handles"
  fi
}

project_root="$(find_project_root "$project_dir")"
if [ -z "$project_root" ]; then
  exit 0
fi

canonical_profile="$project_root/.re-discipline/project-profile.md"
profile_state="$(get_profile_state "$canonical_profile")"
onboard_reminder='Invoke the re-discipline onboard skill before substantive work. Read the canonical profile, active manager adapter, docs indexes, and active campaign masterfiles. Use the shared knowledge server for bounded, citable context.'
recovery_reminder='Legacy or incomplete re-discipline project detected without a supported managed profile. Invoke init-project in migration or recovery mode before substantive work; legacy host profiles are recovery input only.'
checkpoint_reminder='Context is about to compact. If a campaign is active, invoke checkpoint-campaign and preserve current state, decisions, evidence boundaries, source handles, and dead ends. If it is solved, invoke close-campaign.'
subagent_reminder='You are a drafter, not a ratifier. Read the canonical project profile, your exact brief, and its immutable context pack when present. Preserve source citations and epistemic tiers. Write only in the granted workspace; never accept memory, promote truth, change retrieval profiles, close a campaign, commit, push, or spawn another agent.'

case "$event_name" in
  pre-compact)
    printf '%s' '{"systemMessage":'
    json_string_from_text "$checkpoint_reminder"
    printf '%s\n' '}'
    exit 0
    ;;
  subagent-start)
    if [ -f "$canonical_profile" ]; then
      write_hook_context_text "SubagentStart" "$subagent_reminder"
    else
      write_hook_context_text "SubagentStart" "$recovery_reminder"
    fi
    exit 0
    ;;
  post-compact)
    context="Rehydrate re-discipline after compaction. Re-read the canonical profile and active CAMPAIGN.md before substantive work. Use knowledge status or a bounded orientation pack rather than rereading the corpus.$(active_campaign_reminder "$project_root")"
    write_hook_context_text "PostCompact" "$context"
    exit 0
    ;;
  session-start) ;;
  *) exit 0 ;;
esac

recovered_files=""
recovery_warnings=""
memory_mode=""
profile_version="$(sed -n 's/.*re-discipline:shared-laws v\([0-9][0-9.]*\).*/\1/p' "$canonical_profile" 2>/dev/null | head -n 1)"
configuration_recovery "$project_root" "$profile_state"
summary="$(recovery_summary)"
health_summary=""
if [ "$profile_state" = managed ]; then
  health_summary="$(knowledge_health_summary "$project_root")"
fi
if [ -n "$health_summary" ]; then
  if [ -n "$summary" ]; then
    summary="${summary}
${health_summary}"
  else
    summary="$health_summary"
  fi
fi

if [ "$source_name" = compact ]; then
  context="Rehydrate re-discipline after compaction. Re-read the canonical profile and active CAMPAIGN.md before substantive work. Use knowledge status or a bounded orientation pack rather than rereading the corpus.$(active_campaign_reminder "$project_root")"
  if [ -n "$summary" ]; then
    context="${context}
${summary}"
  fi
  write_hook_context_text "SessionStart" "$context"
elif [ ! -f "$canonical_profile" ]; then
  context="$recovery_reminder"
  if [ -n "$summary" ]; then
    context="${context}

${summary}"
  fi
  write_hook_context_text "SessionStart" "$context"
elif [ "$is_codex" = true ]; then
  write_hook_context_file "SessionStart" "$canonical_profile" "$summary"
else
  context="$onboard_reminder"
  if [ -n "$summary" ]; then
    context="${context}

${summary}"
  fi
  write_hook_context_text "SessionStart" "$context"
fi

exit 0
