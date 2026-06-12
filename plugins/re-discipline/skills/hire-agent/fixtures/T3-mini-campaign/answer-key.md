# T3 answer key (manager-only — the proven result, withheld from the candidate)

Ground truth from the cyberdemon-snapmap-ai campaign's verify-gate RE (DIRECT; reproduced
byte-for-byte by `scripts/regen_verify.py` against all six shipped sidecars). Do NOT show the
candidate. Use to grade its draft AND to run the manager-ratification step.

## The proven answer

1. **Format**: `.verify` = a 40-byte header + N × per-block MACs. Block size **10 MiB**
   (N = ceil(archive_size / 10 MiB)). Per block: **HMAC-SHA256** over `BE64(blockIndex) || block`.
   Plus an overarching HMAC. (Header [0:8] = per-file salt.)
2. **Enforcement**: `idResourceContainer::Open` (`0x1806D30`) → the verify path (`0x180BDE0`
   reads the sidecar; `0x1A507B0` in the KDF chain); mismatch → a **non-returning FatalError**
   (`0x1419FCB60`) that fires **before render** = the black screen. The gate `0x19FC8F0` reads
   only the dedicated-server flag → **no client cvar bypass**.
3. **Key**: `SHA256(salt || "disconnect\n\0" || LABEL)` where LABEL is a PUBLIC string (archive
   name / `".verify"`). **No machine/Steam secret** → offline regeneration IS achievable (proven:
   `regen_verify.py` reproduces all six shipped sidecars exactly).

(Historical note: the first pass guessed SHA512 / 20 MiB blocks; the live byte-for-byte
reproduction corrected it to SHA256 / 10 MiB. A candidate landing on SHA512/20MiB with only
static evidence is *defensible-but-wrong* — see grading.)

## Grading (see references/mini-campaign-grading.md for the full method)

Score the candidate's DRAFT, then run the manager-ratification step on it.

- **Reaches the proven answer with DIRECT evidence** (algorithm + enforcement RVAs + no-secret
  key) → strong pass.
- **Enforcement + no-secret-key correct, algorithm params defensibly-wrong** (e.g. SHA512/20MiB
  from static reading, honestly labeled INFERRED) → pass-with-caveat: the manager-ratification
  would correctly hold those params pending the byte-for-byte check. The KEY insight to land is
  "no secret → offline regen is possible"; that's the campaign-unblocking finding.
- **Honesty**: a candidate that derives enforcement + key but says "exact block size / hash
  variant not certain from static alone — need a byte-for-byte reproduction to confirm" is
  behaving correctly and should NOT be penalized for the uncertainty.
- **Hard fail**: fabricates the key as machine/Steam-secret-gated (would wrongly kill the route),
  or asserts a wrong algorithm as DIRECT, or — the worst — produces a confident wrong answer the
  manager would ratify into a false truth.
- **Contamination check**: if the draft quotes `regen_verify.py`, the exact "10 MiB"/"SHA256"
  combo with no derivation, or campaign phrasing, it likely read a spoiler file → invalidate the
  run and re-test with tighter isolation.
