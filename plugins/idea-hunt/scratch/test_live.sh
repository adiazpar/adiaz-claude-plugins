#!/bin/bash
# Test the URL liveness check used by Stage 6.
# Reads URLs (one per line) on stdin, emits "<status>\t<url>" lines.
# Uses GET (not HEAD) because many servers (e.g. HN) reject HEAD with 405.
while IFS= read -r url; do
  [ -z "$url" ] && continue
  code=$(curl -s -L -o /dev/null -w "%{http_code}" --max-time 5 "$url" 2>/dev/null)
  [ -z "$code" ] && code="000"
  printf "%s\t%s\n" "$code" "$url"
done
