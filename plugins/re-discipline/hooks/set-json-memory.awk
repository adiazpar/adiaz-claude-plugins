# Preserve a valid JSON object's text while setting its top-level
# autoMemoryEnabled boolean. Validate with validate-json.awk before invoking.

{
    source = source $0 "\n"
}

function decode_managed_key(raw,    decoded, cursor, character, escape, hexadecimal) {
    decoded = ""
    for (cursor = 1; cursor <= length(raw); cursor++) {
        character = substr(raw, cursor, 1)
        if (character != "\\") {
            decoded = decoded character
            continue
        }
        cursor++
        escape = substr(raw, cursor, 1)
        if (escape != "u") {
            # None of JSON's short escapes can encode a character in the
            # managed identifier, so retain an unmistakable sentinel.
            decoded = decoded "#"
            continue
        }
        hexadecimal = tolower(substr(raw, cursor + 1, 4))
        cursor += 4
        if (hexadecimal == "0061") decoded = decoded "a"
        else if (hexadecimal == "0075") decoded = decoded "u"
        else if (hexadecimal == "0074") decoded = decoded "t"
        else if (hexadecimal == "006f") decoded = decoded "o"
        else if (hexadecimal == "004d") decoded = decoded "M"
        else if (hexadecimal == "0065") decoded = decoded "e"
        else if (hexadecimal == "006d") decoded = decoded "m"
        else if (hexadecimal == "0072") decoded = decoded "r"
        else if (hexadecimal == "0079") decoded = decoded "y"
        else if (hexadecimal == "0045") decoded = decoded "E"
        else if (hexadecimal == "006e") decoded = decoded "n"
        else if (hexadecimal == "0062") decoded = decoded "b"
        else if (hexadecimal == "006c") decoded = decoded "l"
        else if (hexadecimal == "0064") decoded = decoded "d"
        else decoded = decoded "#"
    }
    return decoded
}

END {
    source_length = length(source)
    root_start = 1
    while (root_start <= source_length &&
           substr(source, root_start, 1) ~ /[[:space:]]/) {
        root_start++
    }
    root_end = source_length
    while (root_end > 0 && substr(source, root_end, 1) ~ /[[:space:]]/) {
        root_end--
    }
    if (substr(source, root_start, 1) != "{" ||
        substr(source, root_end, 1) != "}") {
        exit 2
    }

    depth = 0
    in_string = 0
    escaped = 0
    match_count = 0
    property_count = 0
    object_end = 0

    for (cursor = 1; cursor <= source_length; cursor++) {
        character = substr(source, cursor, 1)
        if (in_string) {
            if (escaped) {
                escaped = 0
            } else if (character == "\\") {
                escaped = 1
            } else if (character == "\"") {
                in_string = 0
            }
            continue
        }
        if (character == "\"") {
            start = cursor + 1
            scan = start
            raw_key = ""
            key_escaped = 0
            while (scan <= source_length) {
                candidate = substr(source, scan, 1)
                if (key_escaped) {
                    key_escaped = 0
                    raw_key = raw_key candidate
                } else if (candidate == "\\") {
                    key_escaped = 1
                    raw_key = raw_key candidate
                } else if (candidate == "\"") {
                    break
                } else {
                    raw_key = raw_key candidate
                }
                scan++
            }
            if (depth == 1) {
                lookahead = scan + 1
                while (lookahead <= source_length &&
                       substr(source, lookahead, 1) ~ /[[:space:]]/) {
                    lookahead++
                }
                if (substr(source, lookahead, 1) == ":") {
                    property_count++
                    value_start = lookahead + 1
                    while (value_start <= source_length &&
                           substr(source, value_start, 1) ~ /[[:space:]]/) {
                        value_start++
                    }
                    semantic_key = decode_managed_key(raw_key)
                    if (semantic_key == "autoMemoryEnabled") {
                        # Escaped spellings and duplicate semantic properties
                        # are legal-looking but ambiguous for surgical repair.
                        if (raw_key != semantic_key) {
                            exit 2
                        }
                        if (substr(source, value_start, 4) == "true") {
                            value_end = value_start + 3
                        } else if (substr(source, value_start, 5) == "false") {
                            value_end = value_start + 4
                        } else {
                            exit 2
                        }
                        match_count++
                        policy_start = value_start
                        policy_end = value_end
                    }
                }
            }
            cursor = scan
            continue
        }
        if (character == "{") {
            depth++
        } else if (character == "}") {
            if (depth == 1) {
                object_end = cursor
            }
            depth--
        }
    }

    if (match_count > 1 || object_end == 0) {
        exit 2
    }
    if (match_count == 1) {
        printf "%s%s%s",
            substr(source, 1, policy_start - 1),
            expected,
            substr(source, policy_end + 1)
        exit 0
    }

    trim_end = object_end - 1
    while (trim_end > 0 && substr(source, trim_end, 1) ~ /[[:space:]]/) {
        trim_end--
    }
    if (substr(source, trim_end, 1) == "{") {
        printf "%s\n  \"autoMemoryEnabled\": %s\n%s",
            substr(source, 1, trim_end),
            expected,
            substr(source, object_end)
    } else {
        printf "%s,\n  \"autoMemoryEnabled\": %s%s",
            substr(source, 1, trim_end),
            expected,
            substr(source, trim_end + 1)
    }
}
