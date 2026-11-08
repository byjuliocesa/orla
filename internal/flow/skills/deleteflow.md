---
name: deleteflow
description: Delete an Orla flow, or one of its steps, for good.
disable-model-invocation: true
argument-hint: "[flow] [step]"
metadata:
  orla: system
---

Delete what the user names: $ARGUMENTS

Orla flows are skills in ~/.claude/skills/ (or <cwd>/.claude/skills/) whose front matter has `metadata: orla: flow`. Find the one named; if none is named or the name fits several, list the flows and ask which.

- With only a flow: remove its folder. Leave the skills it uses in place; mention any that no other flow uses, so the user can decide.
- With a flow and a step (the step's "## " title in lower case with dashes): remove that step's section from the flow's SKILL.md, its step file if no other step uses it, and any `branch:` line that pointed to it.

Before deleting anything, say in one line exactly what will be removed and ask the user to confirm. Delete only after a clear yes; on anything else, leave everything as it is.

When done, say in one line what was deleted.
