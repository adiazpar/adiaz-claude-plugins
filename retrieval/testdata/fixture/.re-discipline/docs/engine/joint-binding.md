---
status: promoted
kind: fact
grade: direct
tags: [entities, animation]
evidence: [archive/demon-transforms/reports/R-007.md]
---
# Entity binding to demon joints goes through idAnimatedEntity::AttachJoint

Call idAnimatedEntity::AttachJoint with the joint handle from
GetJointHandle. Works in snapmap-spawned entities as well.
