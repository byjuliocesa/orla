---
name: archiveflow
description: Archive an Orla flow or one of its steps, or bring one back.
disable-model-invocation: true
argument-hint: "[flow] [step] | restore [flow] [step]"
metadata:
  orla: system
---

Archive what the user names, or bring it back: $ARGUMENTS

Orla flows are skills in ~/.claude/skills/ (or <cwd>/.claude/skills/) whose front matter has `metadata: orla: flow`. A step is named by its "## " title in lower case with dashes. The archive is ~/.claude/orla-archive/.

- Archive a flow: move its folder to ~/.claude/orla-archive/flows/.
- Archive a step: cut its whole section (from its "## " line to the next one) out of the flow's SKILL.md and save it as ~/.claude/orla-archive/steps/<flow name>/<step>.md, with a first line `<!-- after: <previous step> -->` (or `<!-- first -->`). Remove `branch:` lines that pointed to it, and note them in that file.
- Restore (the arguments start with "restore"): move the flow's folder back to ~/.claude/skills/, or put the step's section back where its first line says and delete the archived file. Put back the branches noted there.

If a name is missing or fits several, list what can be archived or restored and ask which. When done, say in one line what was archived or restored.
