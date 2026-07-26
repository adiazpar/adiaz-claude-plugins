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

## Recover Managed Knowledge Configuration Separately

When the recovered profile contains
`re-discipline:shared-laws v0.6.0` or newer, it declares that
`.re-discipline/config.json`, required settings, and project host-memory policy
files are managed:

1. Restore each missing tracked file from `HEAD`, including a staged deletion.
2. If a required file has never been tracked, render the current safe template
   atomically and only when absent.
3. Validate bootstrap schema, fixed relative settings paths, commented
   knowledge policy, generated retrieval profile, and both project host
   settings.
4. Never overwrite a malformed existing file, downgrade a newer schema, or
   reconstruct a machine-local grant.
5. Report every restored path and leave the project in read-only degraded mode
   until any malformed file is explicitly repaired.

Do not inspect, copy, merge, or delete Claude or Codex native memory
directories. Recovery restores only project-owned policy files. Explicit
de-initialization removes or replaces the managed shared-law expectation
marker before deleting configuration; as long as the marker survives,
configuration deletion is treated as accidental.

## Verify

- The canonical profile exists and contains confirmed identity frontmatter.
- Both manager adapters resolve it.
- The neutral local-path signature exists and remains untracked.
- Shared laws exist once in the canonical profile and not in either adapter.
- Dispatch framing comes from the canonical file.
- Recovered host-specific instructions live in project-owned manager sections.
- Legacy host profiles are deleted only after meaning preservation succeeds.
- The report states whether recovery was exact, reconciled, or reconstructed.
- Managed bootstrap/settings and selected host memory policy validate when the
  recovered profile requires them.
- No native memory directory or machine-local grant was touched.
