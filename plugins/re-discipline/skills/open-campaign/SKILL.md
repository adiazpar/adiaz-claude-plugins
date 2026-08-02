---
name: open-campaign
description: >-
  Open a re-discipline campaign when asked to begin a substantial
  investigation or implementation effort. Creates validated campaign and root
  work-item records through the shared state engine.
---

# Open A Campaign

A campaign is structured provisional work. Opening it is one engine
transaction, not a directory-scaffolding exercise.

## Prepare

Require:

- a unique lowercase kebab-case slug;
- a precise objective and explicit exclusions;
- observable success and closure criteria;
- at least one root work item with acceptance criteria;
- the manager identity and an idempotency key.

Ask one compact question only when an unresolved choice would materially
change the campaign contract.

## Open Transactionally

Submit `manager_apply` with action `campaign.open`, the exact current
`expectedHeadRevision` and `expectedHeadDigest`, and an idempotency key.
The transaction creates `campaign.json`, root records under `work-items/`, the
event journal, and generated `STATE.md`. It must either publish all of these or
publish none of them.

Do not precreate empty folders, generic evidence categories, or run payload
trees. Link source backlog or goal handles as record relations rather than
copying their prose into campaign state.

Update project navigation only through the engine result or its generated
projection. Preserve unrelated navigation content.

## Verify And Return

Call `state(mode="resume", campaignId=...)` and verify that the objective,
closure bar, root work, revision, and last event agree with the transaction
result. Report the campaign ID, first ready work item, and next valid action.

Do not commit unless the user explicitly asks.

## References

- Record templates: `<plugin-root>/templates/campaign/`.
- Delegation: `delegate`.
- Closure: `close-campaign`.
