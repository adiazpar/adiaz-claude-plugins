# <Claim title — short noun phrase>

**Claim:** <one sentence stating the fact>

**Kind:** atomic | synthesis
- *atomic* — a bedrock fact reproducible from the binary/data (an RVA's behavior, a struct layout, a field offset). Write-once; rarely revised.
- *synthesis* — an interpretation derived from many atomic facts (an encoding rule, a philosophy). The augmentable layer; carries a scope.

**Confidence:** Strong | Moderate | Conditional
*(Promoted only on DIRECT evidence — the Wall. INFERRED findings stay in the campaign. Synthesis claims state their scope.)*

**Scope (synthesis only):** <the bounds within which this holds; what would extend or revise it>

**Validity:**
- Verified: <YYYY-MM-DD>
- DOOM build: see `docs/truth/binaries/build-state.md`
- SnapHak build: <hash or version>
- Codec commit: <git short SHA, if relevant>

**Re-verify trigger:** <what should cause re-checking — e.g. "after any DOOM patch", "never (reproducible from the binary)">

**Depends-on:**
- [<other truth file>](../path)
- (or: none)

**Evidence** — a citation that survives scratch deletion (see the recipe model):
- **Recipe** (reproducible): `tools/re/run.ps1 decompile_fn.py 0xRVA` / a `tests/` test / an oracle call — regenerates the evidence on demand.
- **Archive** (irreproducible): `archive/ground-truth-maps/...` or `archive/evidence/...`.
- **Chronicle** (the derivation/journey): `docs/history/chronicles/<date>-<topic>.md`.

---

## Detail

<as much explanation as the claim requires; tables, code blocks, diagrams>

---

## See also

- [<related truth file>](../path)
- [<chronicle that produced or corrected this>](../../history/chronicles/...)
