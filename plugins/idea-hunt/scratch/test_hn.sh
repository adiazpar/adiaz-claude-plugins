#!/bin/bash
# Probe HN Algolia for complaint patterns.
QUERY="${1:-"i wish there was a tool"}"
curl -sf -G "https://hn.algolia.com/api/v1/search" \
  --data-urlencode "query=${QUERY}" \
  --data-urlencode "tags=comment" \
  --data-urlencode "hitsPerPage=5" \
| jq -r '.hits[] | "[\(.points // 0)] \(.story_title // "(no title)") :: \(.comment_text // .story_text // "" | gsub("<[^>]+>"; "") | .[0:160])"'
