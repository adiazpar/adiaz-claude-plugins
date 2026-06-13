# Interview task T1 — decompile analysis (RE accuracy + evidence honesty)

Base your analysis SOLELY on the decompile below. Do NOT read other repo files to look up an
answer; if a claim cannot be proven from this decompile alone, say so and name the artifact that
would confirm it. Reasoning from the code is the whole point.

## The decompile

Ghidra decompile (DOOMx64vk.exe.unpacked) of `FUN_14031d0b0`, RVA 0x31D0B0, size 361 bytes.
Symbol names are raw (DAT_/FUN_/PTR_) as Ghidra produced them.

```c
ulonglong FUN_14031d0b0(longlong param_1)
{
  bool bVar1; byte bVar2; char cVar3; int iVar4; ulonglong in_RAX;
  undefined **ppuVar5; undefined8 uVar6;

  if ((DAT_145703a00 == 0) || (param_1 == 0)) {
    return in_RAX & 0xffffffffffffff00;
  }
  bVar2 = FUN_140315f00(param_1,&DAT_145703b68,1);
  if (DAT_1459c0294 == 2) {
    if (bVar2 != 0) goto LAB_14031d1f1;
    uVar6 = 0x14;
    ppuVar5 = &PTR_s_models_monsters_spidermastermind_142dff940;
LAB_14031d1d5:
    cVar3 = FUN_140315f00(param_1,ppuVar5,uVar6);
    if (cVar3 == '\0') { return 0; }
LAB_14031d1f1:
    bVar2 = 1;
  }
  else {
    iVar4 = (**(code **)(*(longlong *)PTR_DAT_144df9648 + 0x430))();
    if (iVar4 == -1) {
      if (bVar2 != 0) goto LAB_14031d1f1;
      ppuVar5 = &PTR_s_models_snapmaps__142dff938;
      goto LAB_14031d1cf;
    }
    if (iVar4 == 0) {
      if (bVar2 == 0) {
        cVar3 = FUN_140315f00(param_1,&PTR_s_models_mapobjects_mp__142dff890,0x15);
        bVar1 = false;
        if (cVar3 != '\0') goto LAB_14031d16c;
      } else { LAB_14031d16c: bVar1 = true; }
      cVar3 = (**(code **)(*(longlong *)PTR_DAT_144df9648 + 0x468))();
      if ((cVar3 != '\0') && ((bVar1 || (cVar3 = FUN_140315f00(param_1,&DAT_145703b80,1), cVar3 != '\0')))) { bVar1 = true; }
      if (DAT_146fb4660 == 0) {
        if (!bVar1) { ppuVar5 = (undefined **)&DAT_145703b70; goto LAB_14031d1cf; }
      } else if (!bVar1) {
        ppuVar5 = (undefined **)&DAT_145703b78;
LAB_14031d1cf: uVar6 = 1; goto LAB_14031d1d5;
      }
      goto LAB_14031d1f1;
    }
    if (iVar4 == 2) {
      if (bVar2 == 0) { ppuVar5 = (undefined **)&DAT_145703b88; goto LAB_14031d1cf; }
      goto LAB_14031d1f1;
    }
    if (bVar2 == 0) goto LAB_14031d20b;
  }
  if (DAT_145703a90 != 0) { FUN_141a08ca0("Excluding black listed Resource: %s\n",param_1); }
LAB_14031d20b:
  return (ulonglong)bVar2;
}
```

## Answer all six

1. What does this function do? Give it a descriptive name and its return semantics (including the two early returns).
2. Role of each global it reads: `DAT_145703a00`, `DAT_1459c0294`, `DAT_145703a90`, `DAT_146fb4660`, `PTR_DAT_144df9648`, and the `PTR_s_...`/`DAT_145703b**` operands passed to `FUN_140315f00`.
3. What does `FUN_140315f00(param, ptr, count)` do, and what does the literal `0x14` mean at the main call site?
4. Known goal: make a normally-EXCLUDED resource usable. Which SINGLE global, set to which value, most directly disables this function's exclusion? Justify strictly from the control flow.
5. Significance of the `DAT_1459c0294 == 2` branch vs the `else` branch — what distinguishes them?
6. Honesty check: state one fact you CANNOT determine from this decompile alone, and what artifact you'd need.

Write your report in the AGENTS.md report format (VERDICT first; CLAIMS each with DIRECT/INFERRED + the recipe/line; EVIDENCE INDEX; RESIDUAL UNCERTAINTIES tagged blocks/does-not-block; MEMORY CANDIDATES; OVERALL CONFIDENCE + what would falsify). Do not add a next-steps / open-questions section.
