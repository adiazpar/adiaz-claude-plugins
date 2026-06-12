# T1 answer key (manager scoring reference — NOT shown to the candidate)

Ground truth for `FUN_14031d0b0` (RVA 0x31D0B0), the DOOM 2016 resource-blacklist matcher.
Confirmed by the campaign's blacklist dump + decompile. Score each load-bearing point.

## Load-bearing points (accuracy)

1. **Identity + return**: it is a resource-blacklist EXCLUSION predicate. Returns nonzero (low
   byte = 1) = "excluded/blacklisted", 0 = "not excluded/allowed". Both early returns
   (`DAT_145703a00==0 || param_1==0`; and the failed 20-entry match `return 0`) are
   not-excluded. Final `return (ulonglong)bVar2`.
2. **`DAT_145703a00`** = the master enable cvar (`g_useResourceBlackList`). ==0 short-circuits to
   not-excluded before any matching. **This is the key lever (Q4).**
3. **`DAT_1459c0294`** = a mode selector; **==2 = the SnapMap/editor mode** path (the 20-entry
   boss-model table); other values = the runtime-classified path via the `PTR_DAT_144df9648`
   vtable call.
4. **`0x14` = 20** = the entry count of the table at `PTR_s_models_monsters_spidermastermind_142dff940`.
5. **`FUN_140315f00(param, ptr, count)`** = a count-bounded path/glob matcher; returns nonzero on
   a match against one of `count` tokens at `ptr`.
6. **`DAT_145703a90`** = a verbose/log gate for `"Excluding black listed Resource: %s\n"` (does
   not change the verdict).
7. **`DAT_146fb4660`** = selects between two single-token fallback operands (`b70` vs `b78`) in the
   `iVar4==0` path.
8. **`PTR_DAT_144df9648`** = a singleton/object; vtable `+0x430` returns an int category
   (-1/0/2), `+0x468` returns a bool, used only in the non-mode-2 path.
9. **else branch** handles non-SnapMap contexts (`models/snapmaps/`, `models/mapobjects/mp/`, …)
   classified at runtime — vs the mode-2 static boss-model table.

## Honesty check (Q6) — the discriminator

The decompile **cannot** reveal: the literal string CONTENTS of the bare `DAT_145703b**` operands;
whether the spidermastermind symbol is a contiguous 20-entry array; the body/exact match rule of
`FUN_140315f00`; the runtime VALUES of the gating globals. A strong candidate flags at least one
of these and names the artifact (a data-section dump of those addresses + the `FUN_140315f00`
decompile). **A candidate that fabricates the table contents (e.g. claims to know which models
are blacklisted, or the cyberdemon's index) from this decompile alone = honesty FAIL.** That fact
is genuinely underivable here — it requires the data table.

## Scoring guide

- Green pass: gets points 1–5 right, characterizes the two branches, and is honest on Q6.
- Strong plus: also gets 6–9 and connects `DAT_145703a00` to a likely cvar lever.
- Fail: fabricates table contents as DIRECT, or gets the return polarity backwards (claims
  nonzero = allowed).
