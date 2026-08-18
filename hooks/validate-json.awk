# Small dependency-free JSON/JSONC syntax validator for the startup hook.
# Usage: awk -v allow_comments=0|1 -f validate-json.awk <file>

{
    source = source $0 "\n"
}

function fail() {
    invalid = 1
    return 0
}

function skip_space(    character, pair, closed) {
    while (position <= source_length) {
        character = substr(source, position, 1)
        if (character ~ /[[:space:]]/) {
            position++
            continue
        }
        pair = substr(source, position, 2)
        if (allow_comments && pair == "//") {
            position += 2
            while (position <= source_length &&
                   substr(source, position, 1) != "\n") {
                position++
            }
            continue
        }
        if (allow_comments && pair == "/*") {
            position += 2
            closed = 0
            while (position <= source_length - 1) {
                if (substr(source, position, 2) == "*/") {
                    position += 2
                    closed = 1
                    break
                }
                position++
            }
            if (!closed) {
                return fail()
            }
            continue
        }
        break
    }
    return !invalid
}

function parse_string(    character, escape, hexadecimal) {
    if (substr(source, position, 1) != "\"") {
        return fail()
    }
    position++
    while (position <= source_length) {
        character = substr(source, position, 1)
        if (character == "\"") {
            position++
            return 1
        }
        if (character ~ /[[:cntrl:]]/) {
            return fail()
        }
        if (character == "\\") {
            position++
            if (position > source_length) {
                return fail()
            }
            escape = substr(source, position, 1)
            if (escape == "u") {
                hexadecimal = substr(source, position + 1, 4)
                if (length(hexadecimal) != 4 ||
                    hexadecimal !~ /^[0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]$/) {
                    return fail()
                }
                position += 5
                continue
            }
            if (escape !~ /^["\\\/bfnrt]$/) {
                return fail()
            }
        }
        position++
    }
    return fail()
}

function parse_number(    remainder) {
    remainder = substr(source, position)
    if (!match(remainder, /^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?/)) {
        return fail()
    }
    position += RLENGTH
    return 1
}

function parse_array(    character) {
    position++
    if (!skip_space()) {
        return 0
    }
    if (substr(source, position, 1) == "]") {
        position++
        return 1
    }
    while (!invalid) {
        if (!parse_value() || !skip_space()) {
            return 0
        }
        character = substr(source, position, 1)
        if (character == "]") {
            position++
            return 1
        }
        if (character != ",") {
            return fail()
        }
        position++
        if (!skip_space()) {
            return 0
        }
    }
    return 0
}

function parse_object(    character) {
    position++
    if (!skip_space()) {
        return 0
    }
    if (substr(source, position, 1) == "}") {
        position++
        return 1
    }
    while (!invalid) {
        if (!parse_string() || !skip_space()) {
            return 0
        }
        if (substr(source, position, 1) != ":") {
            return fail()
        }
        position++
        if (!parse_value() || !skip_space()) {
            return 0
        }
        character = substr(source, position, 1)
        if (character == "}") {
            position++
            return 1
        }
        if (character != ",") {
            return fail()
        }
        position++
        if (!skip_space()) {
            return 0
        }
    }
    return 0
}

function parse_value(    character, literal) {
    if (!skip_space()) {
        return 0
    }
    character = substr(source, position, 1)
    if (character == "{") {
        return parse_object()
    }
    if (character == "[") {
        return parse_array()
    }
    if (character == "\"") {
        return parse_string()
    }
    if (character == "-" || character ~ /^[0-9]$/) {
        return parse_number()
    }
    literal = substr(source, position, 5)
    if (substr(literal, 1, 4) == "true") {
        position += 4
        return 1
    }
    if (substr(literal, 1, 5) == "false") {
        position += 5
        return 1
    }
    if (substr(literal, 1, 4) == "null") {
        position += 4
        return 1
    }
    return fail()
}

END {
    source_length = length(source)
    position = 1
    invalid = 0
    if (!parse_value() || !skip_space() || position <= source_length) {
        exit 1
    }
    exit 0
}
