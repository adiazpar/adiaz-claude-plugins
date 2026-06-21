# Market Research Methodology (Claude Code Plugin)

> A six-phase methodology for evaluating commercial viability of digital products. Built for solo founders; adaptable to other founder profiles.

This plugin packages the market-research methodology as installable Claude Code components: one umbrella skill, seven slash commands (Phase 0 demand-discovery plus the six structured agent passes), and seven matching subagents that execute each phase with adapted prompt templates. Multi-candidate state persists as JSONL files in the project's `.claude/market-research/` directory — no external service required. The methodology runs entirely on Claude Code's built-in tools (`WebSearch`, `WebFetch`, and the code-reading tools for the codebase phases) — no external plugins, credentials, or credits.

## Install

**Local development:**
```bash
claude --plugin-dir /path/to/market-research-methodology/plugin
```

**Marketplace (future):**
```
/plugin install market-research
```

After install, reload plugins with `/reload-plugins` and verify the six commands appear in `/help`.

## Tools

The methodology runs entirely on Claude Code's built-in tools — no external services, credentials, or credits:

- **`WebSearch`** — discovery and time-sensitive queries (complaint / willingness-to-pay phrasings, firmographic and segment data, trend triangulation, missed-competitor and disconfirming-evidence searches).
- **`WebFetch`** — a known URL: competitor reviews (G2, Capterra, Trustpilot, app stores), incumbent pricing pages, sold-business listings, trade publications, and free Reddit via `reddit.com/r/<sub>/top.json?limit=N`.
- **`Read` / `Grep` / `Glob` / `Bash`** — reading the codebase in the Phase 2 audit and Phase 6 extraction passes.

Every research pass cites a source URL for each claim and flags where a source was unreachable or the evidence was thin. See the **Research tools** section in SKILL.md for the canonical per-phase reference.

## Persistence (project-local JSONL)

The methodology persists multi-candidate state in three JSONL files under your active project, resolved via `git rev-parse --show-toplevel` (with `pwd` fallback):

```
<your-project>/.claude/market-research/
├── candidates.jsonl       # one line per candidate; latest line wins for updates
├── phase-outputs.jsonl    # one line per (candidate, phase) result, append-only
└── pain-signals.jsonl     # one line per mined pain quote, append-only
```

No external service, no setup, no API keys. The directory is created on first write. State travels with the repo — different projects keep different research ledgers.

**Schemas** (each line is a JSON object):

```jsonc
// candidates.jsonl
{
  "id": "kebab-slug",
  "name": "Display Name",
  "source": "Reddit r/foo | Acquire.com listing | founder's idea | ...",
  "industry": "Retail/SMB|Developer tools|Consumer apps|Infrastructure SaaS|AI tools|Services/Local|Other",
  "status": "mapping|audit-done|angle-named|verified|pressure-tested|decided",
  "verdict": "pass|kill|pending",
  "kill_reason": "one paragraph if killed",
  "updated_at": "2026-05-19T20:30:00Z"
}

// phase-outputs.jsonl
{
  "candidate_id": "kebab-slug",
  "phase": "Map|Audit|Profitability|Adjacent scan|Angle|Verify|Pressure-test|Extraction|Decide",
  "verdict": "pass|kill|conditional|no angle exists",
  "evidence_summary": "short summary; full report saved elsewhere",
  "full_output_path": "docs/research/acmepos-audit.md",
  "agent_run_date": "2026-05-19"
}

// pain-signals.jsonl
{
  "candidate_id": "kebab-slug",
  "pain_phrase": "verbatim quote in user's own words",
  "source_url": "https://...",
  "source_type": "Review|Reddit|Forum|Cold DM|App store|Other",
  "severity": 1,
  "captured_at": "2026-05-19T20:30:00Z"
}
```

**Resolving the path inside a command/agent:**

```bash
PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
MR_DIR="$PROJECT_ROOT/.claude/market-research"
mkdir -p "$MR_DIR"
CANDIDATES="$MR_DIR/candidates.jsonl"
PHASE_OUTPUTS="$MR_DIR/phase-outputs.jsonl"
PAIN_SIGNALS="$MR_DIR/pain-signals.jsonl"
```

**Querying with jq** (latest-line-wins for candidates):

```bash
# All currently-active candidates (not killed)
jq -s 'group_by(.id) | map(max_by(.updated_at)) | map(select(.verdict != "kill"))' "$CANDIDATES"

# Pressure-test verdicts only
jq 'select(.phase == "Pressure-test")' "$PHASE_OUTPUTS"

# Acute pain signals for a specific candidate
jq --arg id "acmepos" 'select(.candidate_id == $id and .severity >= 3)' "$PAIN_SIGNALS"
```

If you'd rather not commit these to git, add `.claude/market-research/` to your `.gitignore`.

## Usage

Seven slash commands cover the methodology — Phase 0 (Demand Discovery, the from-nothing entry point) plus the six structured agent passes (see the skill's "Phase-to-command map" section for the full phase-to-command mapping):

| Command | Phase | Inputs |
|---|---|---|
| `/research-demand-discovery` | 0 (Demand Discovery) | Founder profile, founder constraints, familiar domains, optional starting hypothesis, optional budget |
| `/research-icp-audit` | 2 (Audit) | Project name, root path, language/framework notes |
| `/research-profitability` | 2b (ICP profitability) | Implicit ICP, founder profile, target economics |
| `/research-adjacent-scan` | 2c (Adjacent market scan) | Codebase capabilities, codebase absences, killed ICP, founder domain familiarity, optional candidate list |
| `/research-angle` | 3 (Differentiated angle) | Candidate product, target segment, named alternatives |
| `/research-pressure-test` | 5 (Adversarial pressure test) | Named three-sentence angle from `/research-angle`, named alternatives, optional proxy data |
| `/research-extraction` | 6 alt-path (Extraction evaluation) | Codebase path, prior kills history, founder constraints |

### A greenfield engagement (no codebase, exploring what to build)

```
/research-demand-discovery  →  surfaces ranked problems with evidence
   ↓ recommends #1 candidate
/research-angle  →  names YOUR specific three-sentence angle for the surfaced problem
   ↓
/research-pressure-test  →  finds the killer
   ↓
[decide / commit / kill]
```

Three commands, ~1-2 days. Skips Phases 2/2b/2c/6 (those are brownfield-specific).

### A brownfield engagement (existing codebase, founder unsure who pays)

```
/research-icp-audit  →  identifies implicit ICP from code
/research-profitability  →  evaluates if implicit ICP monetizes
   ├─ pass → /research-angle → /research-pressure-test → verdict
   └─ NO → /research-adjacent-scan → /research-angle → /research-pressure-test → verdict
           └─ no viable adjacency → /research-extraction → verdict
```

Each pass produces a written report (~1500-2500 words). Append the verdict and evidence summary as a line in `<project>/.claude/market-research/phase-outputs.jsonl` (see Persistence section above for the schema).

## What this plugin will not do

- It will not tell you that your idea is good.
- It will not produce confident TAM numbers. The data doesn't exist for most informal markets, and the data that does exist for loud markets is biased.
- It will not replace customer interviews. It tells you which customers are worth interviewing, not what they will say.
- It will not find a market that doesn't exist. If multiple honest passes return "no," the answer is no.
- It will not protect you from over-running. If you run pass after pass without ever committing or stopping, no methodology can save that.

NO is the methodology's most important output. See the skill's "The legitimacy of NO" section for the full elaboration.

## License

MIT.

## Author

Alex Diaz (<alexdiaz0923@gmail.com>)

Built on top of a personal market-research methodology repository. Plugin v0.2.0.
