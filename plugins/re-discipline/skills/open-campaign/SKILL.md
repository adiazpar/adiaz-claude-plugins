---
name: open-campaign
description: >-
  Open a re-discipline campaign when asked to start an investigation, scaffold
  a campaign workspace, or begin focused research. Creates an active campaign directory,
  CAMPAIGN.md, and standard evidence subdirectories with an explicit closure
  bar. Uses lowercase kebab-case slugs.
---

# Open A Campaign

A campaign is the unit of substantial investigation. Its contents remain
provisional until DIRECT evidence is promoted through the Wall.

## Step 1: Validate The Request

Require a unique lowercase kebab-case slug of 3-50 characters. Open a campaign
for work likely to span a session, use live or expensive tools, or require
delegation. Do not scaffold one for a one-line correction or ordinary code
change with no knowledge claim.

## Step 2: Create The Workspace

Create:

```text
active/<slug>/
  scripts/
  analysis/
  artifacts/
  evidence/
  subagents/
```

Resolve the plugin root from this skill's path. Render
`<plugin-root>/templates/campaign-masterfile.md` to
`active/<slug>/CAMPAIGN.md`.

## Step 3: Establish The Closure Bar

Derive discoverable values from the user's request and current docs. Ask one
compact follow-up only for unresolved choices. Fill:

- status line;
- objective and observable definition of solved;
- open questions that gate closure;
- leads and links to truth, history, or backlog sources;
- today's opened date.

Leave dead ends and disposition rows as empty scaffolds. If the campaign came
from `docs/backlog/<slug>.md`, preserve that provenance.

## Step 4: Update The Front Door

Add a relative link and one-line objective under Active campaigns in
`docs/INDEX.md`. Preserve unrelated content and ordering conventions.

## Step 5: Verify And Report

Confirm the masterfile and all subdirectories exist, the closure bar is
testable, and the index link resolves. Report the path and the first unresolved
question.

Do not commit unless the user explicitly asks.

## Reference

- Campaign template: `<plugin-root>/templates/campaign-masterfile.md`.
- Delegation: `delegate`.
- Closure: `close-campaign`.
