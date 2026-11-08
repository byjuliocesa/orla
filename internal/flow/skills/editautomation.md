---
name: editautomation
description: Change an Orla automation.
disable-model-invocation: true
argument-hint: "[automation] [what to change]"
metadata:
  orla: system
---

Change the Orla automation the user names, as they ask: $ARGUMENTS

Automations are folders in ~/.claude/orla-automations/, each with an AUTOMATION.md. If none is named or the name fits several, list them and ask which. If what to change was not said, ask in one short message, then wait.

Keep the format exactly as ~/.claude/skills/newautomation/SKILL.md describes it, and its rules. Change only what was asked. A running automation picks up a change when the owner turns it off and on again: say so.

When done, say in one line what changed.
