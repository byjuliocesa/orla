---
name: editflow
description: Change an Orla flow, or one of its steps.
disable-model-invocation: true
argument-hint: "[flow] [step] [what to change]"
metadata:
  orla: system
---

Change an Orla flow as the user asks: $ARGUMENTS

Orla flows are skills in ~/.claude/skills/ (or <cwd>/.claude/skills/) whose front matter has `metadata: orla: flow`. Find the one named; if none is named or the name fits several, list the flows and ask which. A second word that matches a step (its "## " title in lower case with dashes) means only that step changes.

If what to change was not said, ask in one short message, then wait.

Keep the format exactly as ~/.claude/skills/newflow/SKILL.md describes it, `next:` included for steps on a conditional path. Change only what was asked. When a change touches a skill other flows also use, say so before changing it.

When done, say in one line what changed.
