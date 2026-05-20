#!/bin/bash
# Probe Reddit's public JSON. Custom UA is REQUIRED — default UAs are 403'd.
SUB="${1:-SomebodyMakeThis}"
QUERY="${2:-app}"
UA="idea-hunt/1.0 (Claude Code command; +https://github.com/adiaz)"
URL="https://www.reddit.com/r/${SUB}/search.json?q=${QUERY}&restrict_sr=1&sort=top&t=year&limit=5"
HTTP_CODE=$(curl -s -A "${UA}" -o /tmp/reddit_test.json -w "%{http_code}" "${URL}")
if [ "${HTTP_CODE}" != "200" ]; then
  echo "BLOCKED: HTTP ${HTTP_CODE} from Reddit (expected — Reddit is best-effort Tier 2)"
  exit 0
fi
jq -r '.data.children[].data | "[\(.score)] \(.title) — \(.selftext // "" | .[0:140])"' /tmp/reddit_test.json
