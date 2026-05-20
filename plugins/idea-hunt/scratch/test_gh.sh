#!/bin/bash
# Probe GitHub for trending repos by created date.
QUERY="${1:-"language:python stars:>500"}"
SINCE="${2:-$(date -u -v -90d +%Y-%m-%d 2>/dev/null || date -u -d '90 days ago' +%Y-%m-%d)}"
curl -sf -G "https://api.github.com/search/repositories" \
  -H "Accept: application/vnd.github+json" \
  --data-urlencode "q=${QUERY} created:>${SINCE}" \
  --data-urlencode "sort=stars" \
  --data-urlencode "order=desc" \
  --data-urlencode "per_page=5" \
| jq -r '.items[] | "\(.stargazers_count)⭐ \(.full_name) — \(.description // "(no description)")"'
