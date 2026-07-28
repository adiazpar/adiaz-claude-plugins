<!-- re-discipline:claude-adapter v0.7.0 -->
# {{PROJECT_NAME}} - Claude Code Manager Adapter

Claude Code automatically loads this host adapter. The imported project
profile is the single source of shared project facts and re-discipline laws.

@../.re-discipline/project-profile.md

After loading it, follow its Session Start contract and use the `onboard`
skill. `.re-discipline/config.json` selects the project memory mode. New
projects use `shared-only`, represented by `autoMemoryEnabled: false` in the
preserved project `.claude/settings.json`; this does not modify Claude's
machine-local memory directory. Project-owned Claude-specific notes may follow
this managed block.
<!-- re-discipline:claude-adapter:end -->
