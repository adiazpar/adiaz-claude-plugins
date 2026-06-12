# Research checklist — what to discover before drafting a provider config

Discover each item below for the candidate CLI. WebSearch the official docs for current syntax,
then VERIFY from `<cli> --help` / `<cli> <subcommand> --help` after install. Never trust training
data for flag names — CLIs churn. Record findings in `CANDIDATE.md`; map them into
`config-draft.json` per the field map at the bottom.

## Items to discover

1. **Non-interactive / exec entry point** — the subcommand or flag that runs one prompt and exits
   (Codex: `codex exec "<prompt>"`; Gemini: `gemini -p "<prompt>"`). The whole framework assumes
   a one-shot batch invocation, not an interactive REPL.
2. **Prompt delivery + stdin behavior** — is the prompt an argument, or stdin? Does the CLI block
   reading stdin when it is a non-TTY pipe? (Codex does — the dispatcher pipes `$null` to force
   EOF. Check whether the candidate needs the same.)
3. **Working-directory flag** — the equivalent of Codex's `-C <dir>` (run with the repo as root).
4. **Skip-permissions / skip-sandbox flag** — the flag that lets the agent run tools/commands
   without per-action approval AND (where applicable) enables MCP tool calls non-interactively.
   This becomes `bypass_args`. (Codex: `--dangerously-bypass-approvals-and-sandbox`; Gemini's
   likely `--yolo` — VERIFY.) Many CLIs auto-cancel MCP calls without this; test it.
5. **Sandbox mode flags** — the safe fallback (`sandbox_args`) for a `-Sandboxed` static run, if
   the CLI offers one.
6. **Model selection flag** — the `-m`/`--model` equivalent (`model_flag`).
7. **Last-message capture** — a flag to write the final message to a file (Codex: `-o <file>`),
   used as the report fallback when the agent doesn't write `report.md` itself.
8. **MCP config format + location** — where the CLI reads MCP servers (Codex: `~/.codex/config.toml`
   `[mcp_servers.*]` TOML; Gemini: `.gemini/settings.json` JSON — VERIFY). Note timeout fields
   (the daemon's `test_rawmap` needs a long tool timeout). Local runners may not support MCP at all.
9. **Instructions-file name** — the CLI's CLAUDE.md-equivalent that it auto-reads (Codex: `AGENTS.md`;
   Gemini: `GEMINI.md`). This is `instructions_file`. Promote materializes the shared contract there.
10. **Auth method** — login command or env var. A USER step — never automate; pause and instruct.
11. **Local-runner specifics** (Qwen/Ollama/LM Studio/llama.cpp) — the `command` is the local
    runner or an OpenAI-compatible endpoint shim; confirm it accepts the same exec/prompt shape.
    Capability is the real risk here — the interview is what catches a too-small model.

## Field map → config-draft.json

| Discovery | config field |
|---|---|
| exec entry + prompt + working-dir + capture | `command` + `args` template (tokens `{model_args} {root} {policy_args} {lastmsg} {prompt}`) |
| model selection flag | `model_flag` (+ leave `model_preference: []`, `default_model: null` to ride the CLI default) |
| skip-permissions flag | `bypass_args` (array) + `bypass_default: true` |
| sandbox fallback flags | `sandbox_args` (array; may be `[]` if none) + `sandbox` / `approval_policy` if templated |
| instructions-file name | `instructions_file` |

Leave `promoted` absent/false in the draft — only `decide-agent promote` sets it.
