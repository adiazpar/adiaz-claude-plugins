# Profile Recovery

Use recovery when re-discipline markers remain but project identity is missing.
Identity is declared, not inferred, so recover exact content before attempting
reconstruction.

## Search Git First

Check history for all profile generations:

```powershell
git log --oneline -- .re-discipline/project-profile.md
git log --oneline -- .claude/project-profile.md
git log --oneline -- .codex/project-profile.md
```

Restore the newest canonical profile when available. If only a legacy profile
exists, restore it to a temporary review state, classify its project facts and
host-specific notes, then migrate it through the drop-in procedure. Do not
replace a surviving file before showing what recovery found.

## Reconcile Surviving Copies

If Claude and Codex profiles both survive but the canonical profile does not,
compare them. Matching domain facts can seed the canonical profile. Conflicting
facts require current source evidence or user confirmation. Preserve
host-specific sections as project-owned notes outside the managed block in the
matching manager adapter. Recreate generic laws only in the canonical
shared-law block.

## Reconstruct Only As A Fallback

When git has no usable identity, reconstruct derivable sections from
`docs/INDEX.md`, README files, truth indexes, source/reference directories,
tooling, config, and targeted memory indexes. Present the best draft and ask
the user to confirm `name`, `type`, `framing`, and mission before writing.
Never silently invent those fields.

## Recover Local Paths Separately

Do not recover machine-local values from Git history. If
`.re-discipline/local-paths.md` is missing, reconcile surviving
`.claude/local-paths.md` and `.codex/local-paths.md` through the drop-in
procedure. If no local file survives, render the neutral signature with a
commented assignment instruction and ask for only the values required by
current tools. Ensure the neutral file is ignored before writing values.

## Verify

- The canonical profile exists and contains confirmed identity frontmatter.
- Both manager adapters resolve it.
- The neutral local-path signature exists and remains untracked.
- Shared laws exist once in the canonical profile and not in either adapter.
- Dispatch framing comes from the canonical file.
- Recovered host-specific instructions live in project-owned manager sections.
- Legacy host profiles are deleted only after meaning preservation succeeds.
- The report states whether recovery was exact, reconciled, or reconstructed.
