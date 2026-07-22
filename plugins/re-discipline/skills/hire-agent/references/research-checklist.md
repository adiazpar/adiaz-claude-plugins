# External CLI Research Checklist

Use current official provider documentation, then verify behavior against the
installed CLI's help and a harmless dry run. Record exact version and date.

## Mechanics

1. Non-interactive entry point and prompt delivery.
2. Standard-input behavior for a non-TTY process.
3. Working-directory option; prefer the drafter workspace so nested
   `AGENTS.override.md` is discovered.
4. Sandbox and approval controls. Record the safest autonomous mode that still
   supports the required tools.
5. Any explicit sandbox-bypass flag. Store it as `bypass_args`, but never make
   bypass the default.
6. Model-selection flag and how the CLI handles an unknown model id.
7. Final-message or structured-output capture.
8. MCP/tool configuration format, scope, and timeout controls.
9. Automatically discovered instruction filenames and precedence.
10. Authentication method. Authentication is always a user action.
11. Exit codes, timeout behavior, output truncation, and logging.
12. Current provider guidance for prompting the exact model being evaluated.

## Config Field Map

| Discovery | Provider field |
|---|---|
| executable | `command` |
| invocation template | `args` |
| model flag | `model_flag` |
| preferred/current model | `model_preference`, `default_model` |
| safe policy | `sandbox_args` |
| explicit unsafe policy | `bypass_args` |
| drafter law | `instructions_file` |
| provider prompt overlay | `profile` |

The dispatcher supports `{model_args}`, `{policy_args}`, `{root}`,
`{workspace}`, `{brief}`, `{report}`, `{lastmsg}`, and `{prompt}` tokens.

Do not add credentials, access tokens, local secrets, or unverified flags to a
draft config. Keep `promoted: false` until `decide-agent` applies the user's
decision.
