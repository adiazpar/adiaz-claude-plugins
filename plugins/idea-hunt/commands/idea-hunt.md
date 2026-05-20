---
description: Find a defensible software-product idea from free public web signals. Optional scope arg. Optional --depth=light|medium|deep (default medium).
argument-hint: [scope] [--depth=light|medium|deep]
---

# /idea-hunt

You are running the `idea-hunt` command. Your job: produce ONE ranked software-product-idea recommendation, backed by cited public-web evidence, using the 7-stage pipeline below. You MUST follow every stage in order. You MUST NOT skip the citation, diversity, and liveness checks in Stage 6 — they are non-negotiable.

**Inputs (from `$ARGUMENTS`):**
- `scope`: optional freeform string (e.g. `"developer tools"`). If absent, run in cold/open mode.
- `--depth=light|medium|deep`: optional, default `medium`.

**Tools you will use:** `Bash` (for `curl`, `jq`, `python3` snippets below), `WebFetch` (for HTML pages where `curl` won't help), the `idea-hunt-scoring` skill (for rubric reference), and file I/O for the history log.

**Funnel targets per depth:**

| Depth | Stage 1 | Stage 2 | Stage 3 | Stage 4 | Stage 5 | Total fetches |
|---|---|---|---|---|---|---|
| light | 20 | 5 | 3 | 2 | 1 | ~10-15 |
| medium (default) | 40 | 10 | 5 | 3 | 1 | ~25-30 |
| deep | 60 | 20 | 8 | 5 | 1 + disprove pass | ~40-50 |

---

## Stage 0 — Parse args, load history

1. Parse `$ARGUMENTS`. Set `SCOPE` (may be empty) and `DEPTH` (default `medium`). Reject unknown depth values with an explicit error.

2. Resolve project root and ensure history file exists. History is project-scoped: it lives under `<project_root>/.claude/idea-hunt-history.jsonl` so it travels with the repo, not with the user's home dir.

```bash
PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
HISTORY_FILE="$PROJECT_ROOT/.claude/idea-hunt-history.jsonl"
mkdir -p "$(dirname "$HISTORY_FILE")"
touch "$HISTORY_FILE"
```

3. Load past winners. The file is the ONLY ground truth for "previously surfaced" — never infer past winners from conversation context.

```bash
PAST_WINNERS=$(jq -r 'select(.winner.name) | .winner.name' "$HISTORY_FILE" 2>/dev/null)
echo "$PAST_WINNERS"
```

Hold the resulting list (may be empty) in working memory for Stage 1 deduplication.

---

## Stage 1 — Discover candidates

Hit the source tiers below. Target counts come from the depth table above. Every candidate you record MUST include at least one source URL — no URL, no candidate.

### Tier 1 — reliable, always hit

If a Tier 1 source returns non-2xx, retry once with a short backoff. If the retry also fails, mark it failed (NOT partial). **If both Tier 1 sources (HN Algolia AND GitHub Search) fail, abort the entire run with an explicit error.** Do not proceed to filtering on the surviving Tier 2/3 data — the recommendation would not be defensible.

**HN Algolia (complaint mining):**

```bash
curl -sf -G "https://hn.algolia.com/api/v1/search" \
  --data-urlencode "query=<scope or wish phrase>" \
  --data-urlencode "tags=comment" \
  --data-urlencode "hitsPerPage=20" \
| jq -r '.hits[] | "\(.objectID)\t\(.points // 0)\t\(.story_title // "(no title)")\t\((.comment_text // .story_text // "") | gsub("<[^>]+>"; "") | .[0:300])"'
```

Run multiple variants — at minimum: `"i wish there was a tool"`, `"is there a tool that"`, `"alternative to <known leader in scope>"`, and the literal scope phrase if one was passed. Record each comment's HN URL: `https://news.ycombinator.com/item?id=<objectID>`.

**HN Algolia (Show HN / Ask HN):**

```bash
curl -sf -G "https://hn.algolia.com/api/v1/search" \
  --data-urlencode "query=<scope>" \
  --data-urlencode "tags=story" \
  --data-urlencode "hitsPerPage=20"
```

Use these as evidence of recent attempts and as competitor signals.

**GitHub Search API (new high-momentum projects):**

```bash
curl -sf -G "https://api.github.com/search/repositories" \
  -H "Accept: application/vnd.github+json" \
  --data-urlencode "q=<scope> created:>$(date -u -v -90d +%Y-%m-%d 2>/dev/null || date -u -d '90 days ago' +%Y-%m-%d)" \
  --data-urlencode "sort=stars" \
  --data-urlencode "order=desc" \
  --data-urlencode "per_page=10"
```

**Budget: ≤10 GitHub calls total per run.** Watch `X-RateLimit-Remaining` in response headers. If it hits 3, stop calling GitHub for this run. (Unauth Search API is ~10 req/min — stay well under.)

### Tier 2 — best-effort, set UA, skip on failure

For each tier-2 source, attempt the fetch. On non-200 (403, 429, Cloudflare challenge, timeout), log to a `partial_sources` list and continue. Do NOT retry tier 2 sources; they fail or they don't.

**Reddit public JSON (REQUIRES custom UA):**

```bash
UA='idea-hunt/1.0 (Claude Code command)'
SUB=<subreddit>  # e.g. SomebodyMakeThis, lightbulb, or a topic-specific sub
HTTP_CODE=$(curl -s -A "$UA" -o /tmp/reddit_$SUB.json -w "%{http_code}" \
  "https://www.reddit.com/r/$SUB/search.json?q=<query>&restrict_sr=1&sort=top&t=year&limit=20")
if [ "$HTTP_CODE" = "200" ]; then
  jq -r '.data.children[].data | "\(.permalink)\t\(.score)\t\(.title)\t\(.selftext // "" | .[0:300])"' /tmp/reddit_$SUB.json
else
  echo "REDDIT_BLOCKED $SUB $HTTP_CODE" >&2
fi
```

Always hit: `r/SomebodyMakeThis`, `r/lightbulb`. If scope is set, also hit one topic-specific subreddit you choose for that scope. Record each post's URL as `https://www.reddit.com<permalink>`.

**Indie Hackers, YC RFS, Product Hunt:**

Use `WebFetch` on:
- `https://www.indiehackers.com/`
- `https://www.ycombinator.com/rfs`
- `https://www.producthunt.com/` (and the topic page for the scope, if applicable)

If `WebFetch` returns boilerplate-only or fails, log to `partial_sources` and continue.

### Tier 3 — try, skip silently if blocked

- AlternativeTo, App Store / Play Store review pages, Google Trends: attempt via `WebFetch`. If output looks like a Cloudflare challenge page, a JS shell, or contains no meaningful content, skip — do NOT add to `partial_sources` (these are expected to fail).

### Discovery techniques to apply across all sources

- **Trend-chasing:** what's surging in HN stories, GitHub stars-in-last-90-days, Product Hunt
- **Unmet-demand mining (PRIMARY):** phrases like "I wish there was…", "is there a tool that…", "why doesn't anyone…", "I had to build…"
- **Arbitrage:** products that work in one niche/region/platform but not adjacent ones

### Candidate record

For every candidate you keep, record:

```
- name: <short, distinctive>
- problem_statement: <one sentence>
- raw_signals: [list of (source_url, snippet) tuples]
- raw_count: <integer>
```

Deduplicate against `PAST_WINNERS` using the fuzzy-match snippet (see Stage 2). Tag matches `previously_surfaced` and drop them — do NOT carry them into Stage 2.

**End of Stage 1.** You should have approximately `<Stage 1 target>` candidates. Output a brief "Stage 1: N candidates from M sources" status line in the chat.

---

## Stage 2 — Demand filter (Stage 1 count → Stage 2 target)

Score every Stage 1 candidate on **demand** and **pain intensity** only, using the rubric in `skills/idea-hunt-scoring/SKILL.md`. (Invoke that skill now: `Skill: idea-hunt-scoring`.)

Drop candidates with `demand < 3` or `pain < 3`. Keep the top `<Stage 2 target>` candidates by `demand × pain`.

For each surviving candidate, write a one-line evidence note: which signal(s) drove the demand score, with the source URL.

**Past-winner dedup:** for each surviving candidate's name, run the fuzzy-match check against `PAST_WINNERS`:

```bash
python3 - "<candidate_name>" "$HISTORY_FILE" <<'PY'
import sys, difflib, re, os, json
def norm(s):
    s = s.lower()
    s = re.sub(r'[^\w\s]', '', s)
    s = re.sub(r'\s+', ' ', s).strip()
    return s
new = sys.argv[1]
hist_path = sys.argv[2]
if not os.path.exists(hist_path):
    print("NO_MATCH"); sys.exit(0)
for line in open(hist_path):
    line = line.strip()
    if not line: continue
    try:
        w = json.loads(line)["winner"]["name"]
    except Exception:
        continue
    if difflib.SequenceMatcher(None, norm(new), norm(w)).ratio() >= 0.85:
        print(f"MATCH {w}"); sys.exit(0)
print("NO_MATCH")
PY
```

If output starts with `MATCH`, drop the candidate. (Fallback if `python3` is unavailable: lowercase + strip-punctuation + exact match on the normalized strings.)

End of stage: emit `Stage 2: <N> → <M> candidates (demand+pain filter)` to chat.

---

## Stage 3 — Competition + monetization filter (Stage 2 → Stage 3)

For each surviving candidate, do targeted research:

1. **Competition:** search for existing solutions. Use HN Algolia stories for the candidate's domain, GitHub Search API for related repos, and `WebFetch` against the top 2 named competitors' homepages. Identify (a) the strongest 2-3 competitors and (b) at least one explicit wedge (gap, weakness, complaint pattern) — or note "no clear wedge".

2. **Monetization:** check whether named competitors have a public pricing page. Use `WebFetch` on `<competitor>/pricing` or equivalent. Record observed price points.

3. Score `competition_strength` and `monetization` against the rubric.

**Hard gates (drop immediately):**
- `monetization < 2`
- `competition_strength == 5` AND no documented wedge

Keep top `<Stage 3 target>` candidates by current partial score (`demand × pain × monetization × ((6−competition)²/5)`).

Emit `Stage 3: <N> → <M> candidates (competition+monetization filter)`.

---

## Stage 4 — Why-now + distribution filter (Stage 3 → Stage 4)

For each survivor:

1. **Why-now:** identify the specific recency unlock — new model capability, new public API, new regulation, new platform launch — that makes this idea viable *now*. Cite the specific event with a URL. If none, set `why_now=1.0`. If the recency works against the idea, set `why_now < 1.0`.

2. **Distribution:** identify where the customers cluster. Specific subreddits, Discord communities, conferences, newsletters, hashtags. Higher concentration = higher score. Cite at least one specific channel URL.

Score `why_now` and `distribution`. Compute the full score with the formula:

```
score = demand × pain × monetization × ((6 − competition_strength)² / 5) × distribution × why_now
```

In `--depth=deep`: if `why_now < 1.0`, require an explicit justification paragraph or eliminate.

Keep top `<Stage 4 target>` candidates by full score.

Emit `Stage 4: <N> → <M> candidates (why-now+distribution filter)`.

---

## Stage 5 — Deep validation, pick winner (Stage 4 → 1)

For each remaining candidate:

1. Re-read the strongest source quotes; tighten the evidence list. Drop unsubstantiated claims.
2. Confirm the wedge: write 2-3 sentences on *exactly* what about the existing market is wrong, and how the new product is different.
3. Confirm the why-now: write 1-2 sentences on what changed and when.
4. Write the 80-word build sketch — what shipping v0.1 looks like, concretely.
5. Write the distribution playbook — 3 specific channels with URLs.

**In `--depth=deep` only: disprove pass.** For the top-scoring candidate, actively try to kill it:
- Find the silent competitor: search HN/GitHub for repos in this exact space older than 2 years; if you find an active one with >1000 stars or >$100k revenue evidence, the wedge claim is weaker.
- Find the failed predecessor: search "shut down" + the candidate domain on HN; if a predecessor died of an unfixable structural problem, the candidate inherits it.
- Find the structural blocker: ask "why hasn't this been built?" Look for regulatory, platform, or distribution reasons.

Record findings in a `disprove_log` array (used in the output). If disprove pass kills the top candidate, promote the runner-up and re-run disprove against it.

Select the highest-scoring surviving candidate as the **Winner**. The rest become **Honorable Mentions** (1 for light, 2 for medium, 4 for deep).

Emit `Stage 5: <N> → 1 (deep validation)`.

---

## Stage 6 — Verify citations, write report, append to history

This stage has two mechanical sub-checks (6b URL liveness, 6c source diversity) plus drafting (6a), output (6d), and history append (6e). None of the checks are optional. Failures are loud, not silent.

### 6a — Draft the Winner section

Write the Winner section in the output format below. Every claim in Problem, Evidence, Competition, Why-now, and Monetization MUST carry a `[source](url)` link. Claims without inline citations will be stripped in 6b — you may as well not write them.

### 6b — URL liveness check

Extract every `https?://` URL from the Winner section and probe each with a GET (not HEAD — many servers, including HN, reject HEAD with 405):

```bash
# Pipe each URL through this loop; keep only 2xx/3xx.
# Uses GET (not HEAD) because many servers (e.g. HN) reject HEAD with 405.
for url in <urls...>; do
  code=$(curl -s -L -o /dev/null -w "%{http_code}" --max-time 5 "$url" 2>/dev/null)
  [ -z "$code" ] && code="000"
  echo -e "${code}\t${url}"
done | awk -F'\t' '$1 !~ /^[23][0-9][0-9]$/'
```

Any URL printed by the `awk` is dead. Strip the entire claim (the `[text](dead-url)` link AND its parent sentence) from the Winner section.

### 6c — Source-diversity check

Count distinct *domains* (not URLs) cited in the surviving Winner section:

```bash
# given a list of URLs:
printf "%s\n" "${winner_urls[@]}" | awk -F/ '{print $3}' | sort -u | wc -l
```

If the count is **less than 3**, the Winner is under-diverse. **Fail the run** with an explicit error:

```
ERROR: Winner candidate "<name>" cites only <N> distinct domains (need ≥3 after stripping dead links).
       Promoting runner-up: "<next>". Re-running Stage 6 against runner-up.
```

Then promote the highest-scoring honorable mention and re-run 6a-6c against it. If the runner-up also fails, fail the entire run — do not lower the bar.

### 6d — Emit the report

Output a single markdown block in chat with this structure:

```
# Idea Hunt — <YYYY-MM-DD> — scope: <scope or "open"> — depth: <depth>

## 🏆 Winner: <name>
**Score: <total>** | demand <d> · pain <p> · monetization <m> · competition <c> · distribution <dist> · why-now ×<w>

**Problem.** <paragraph, every claim cited>
**Evidence.** <bullet list, recency-stamped, cited>
**Competition.** <existing players, where they fall short, the wedge, cited>
**Why now.** <recent enabler, cited>
**Monetization path.** <pricing model + comparable in space, cited>
**Build sketch.** <80-word concrete v0.1>
**Distribution playbook.** <3 specific channels, with URLs>

## Honorable mentions
1. <name> — score <n> — <one line why it didn't win>
[2. <name> — score <n> — <one line> (medium+)]
[3-4. (deep only)]

## Funnel
- Stage 1 (discover): <N> candidates from <sources, comma-separated>
- Stage 2 (demand+pain): <N → M>
- Stage 3 (competition+monetization): <N → M>
- Stage 4 (why-now+distribution): <N → M>
- Stage 5 (deep validation): <N → 1>
- Partial sources (if any): <list>

## Disprove log  (deep mode only)
- <each disprove attempt: what was tried, what was found>

## History
Appended winner to <project_root>/.claude/idea-hunt-history.jsonl
```

### 6e — Append to history

Append the Winner record to history. Use a single-line JSON object (no pretty-printing — the file is JSONL):

```bash
python3 - "$HISTORY_FILE" <<'PY'
import json, sys
record = {
    "date": "<YYYY-MM-DD>",
    "scope": "<scope or empty>",
    "depth": "<depth>",
    "winner": {
        "name": "<winner name>",
        "score": <total>,
        "dimensions": {
            "demand": <d>,
            "pain": <p>,
            "monetization": <m>,
            "competition": <c>,
            "distribution": <dist>,
            "why_now": <w>
        },
        "sources": ["<url>", "<url>", "<url>"]
    }
}
path = sys.argv[1]
with open(path, "a") as f:
    f.write(json.dumps(record) + "\n")
print(f"Appended to {path}")
PY
```

Fill in every `<...>` placeholder with real values before executing. **Do not append a record with placeholder values.** If you cannot fill a field, the run failed earlier and you should not be in Stage 6e.

---

## Final reminders (hard rules, recap)

1. **No source URL = claim stripped.** Don't write claims you can't cite.
2. **Citation rule applies to the Winner section only.** Funnel summary and Honorable Mentions need not be individually cited.
3. **history.jsonl is the ONLY ground truth for past winners.** Don't infer past winners from conversation context.
4. **Tier 1 sources failing = stop and report.** Never fabricate or pad results.
5. **Source diversity < 3 distinct domains on the Winner = promote runner-up.** Don't lower the bar.
