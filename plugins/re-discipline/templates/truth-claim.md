# <Claim title - short noun phrase>

**Claim:** <one value-precise sentence>

**Kind:** atomic | synthesis

**Confidence:** Strong | Moderate | Conditional

**Scope (synthesis only):** <where the claim holds and where it does not>

**Validity:**
- Verified: <YYYY-MM-DD>
- Subject/source revision: <version, hash, build, or primary artifact>
- Implementation revision: <commit or version, when relevant>

**Re-verify trigger:** <the change that should cause a new check>

**Depends-on:**
- [<other truth file>](../path)
- or: none

## Verification

- **Source:** <maintained primary source path and exact value, or none>
- **Test or fixture:** <permanent test and maintained fixture, or none>
- **Recipe:** `<runnable command against the named subject revision>`, or none
- **Provenance:** `docs/history/chronicles/<date>-<topic>.md`

Only DIRECT evidence supports promotion. At least one of Source, Test or
fixture, or Recipe must let a future manager recheck the claim after campaign
scratch is deleted. Provenance explains derivation; it is not empirical support.

## Detail

<The explanation, exact values, tables, and necessary boundaries.>

## See Also

- [<related truth>](../path)
- [<producing chronicle>](../../history/chronicles/...)
