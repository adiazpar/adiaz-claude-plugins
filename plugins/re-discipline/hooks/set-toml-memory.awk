# Preserve TOML text while setting one boolean in a named table.
# Exits 2 for duplicate/ambiguous managed tables or fields.

{
    lines[NR] = $0
}

END {
    active = ""
    section_count = 0
    key_count = 0
    section_start = 0
    section_end = NR + 1

    for (line_number = 1; line_number <= NR; line_number++) {
        candidate = lines[line_number]
        sub(/#.*/, "", candidate)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", candidate)
        if (candidate ~ /^\[[^]]+\]$/) {
            table = substr(candidate, 2, length(candidate) - 2)
            if (active == wanted_section && section_end == NR + 1) {
                section_end = line_number
            }
            active = table
            if (table == wanted_section) {
                section_count++
                section_start = line_number
            }
            continue
        }
        if (active == wanted_section &&
            candidate ~ ("^" wanted_key "[[:space:]]*=")) {
            if (candidate !~ ("^" wanted_key "[[:space:]]*=[[:space:]]*(true|false)$")) {
                exit 2
            }
            key_count++
            key_index = line_number
        }
    }

    if (section_count > 1 || key_count > 1) {
        exit 2
    }
    if (section_count == 0) {
        for (line_number = 1; line_number <= NR; line_number++) {
            print lines[line_number]
        }
        if (NR > 0 && lines[NR] != "") {
            print ""
        }
        print "[" wanted_section "]"
        print wanted_key " = " expected
        exit 0
    }
    if (key_count == 1) {
        prefix = lines[key_index]
        sub(/true|false/, expected, prefix)
        lines[key_index] = prefix
        for (line_number = 1; line_number <= NR; line_number++) {
            print lines[line_number]
        }
        exit 0
    }

    for (line_number = 1; line_number <= NR; line_number++) {
        if (line_number == section_end) {
            print wanted_key " = " expected
        }
        print lines[line_number]
    }
    if (section_end == NR + 1) {
        print wanted_key " = " expected
    }
}
