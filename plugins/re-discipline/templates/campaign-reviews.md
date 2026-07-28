# Review Ledger: <campaign slug>

> The campaign's record of what each drafter report was found to contain. It is
> a ledger, not state: `CAMPAIGN.md` says where the campaign is now, this says
> what has been checked and what it cost.
>
> Kept separate because the two grow at different rates. Campaign state is
> rewritten each checkpoint and stays small enough to re-read cold; the ledger
> only ever appends. Holding both in one file is why a long campaign's
> masterfile becomes too large to serve its own purpose.

## Reviews

| Date | Report | PROMOTE | HOLD | DROP | BLOCK | Promoted to |
|---|---|---|---|---|---|---|
| <YYYY-MM-DD> | `subagents/<key>/report.md` | 0 | 0 | 0 | 0 | - |

Each row mirrors the `**Review:**` stamp written into the report itself. The
stamp is authoritative - it travels with the content it qualifies and is what
retrieval reads. This table exists so a manager can see the whole campaign's
review state without opening every report.

## Unresolved Holds

| Report | Held claim | Decisive observation still needed | Destination if the campaign closes first |
|---|---|---|---|
| `subagents/<key>/report.md` | <claim> | <what would settle it> | `docs/backlog/<item>.md` |

HOLD is the disposition with no destination of its own. PROMOTE reaches truth,
DROP reaches the chronicle's dead ends, BLOCK becomes an open question - a held
claim survives only as long as its campaign unless something is written here.

Empty this table at every checkpoint by resolving rows, not by deleting them.

## Corrections And Blocks

<Claims a review disproved or that conflict with current truth, with the
primary evidence. Blocks that require reconciling two sources before either
side can move.>
