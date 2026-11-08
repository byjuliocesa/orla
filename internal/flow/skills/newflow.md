---
name: newflow
description: Create an Orla flow, a skill listing the steps the AI follows for a kind of task.
disable-model-invocation: true
argument-hint: "[what the flow is for]"
metadata:
  orla: system
---

Create an Orla flow from what the user describes: $ARGUMENTS

If nothing was described, ask in one short message what kind of task the flow is for and what its steps are, then wait for the answer.

Write it as a Claude Code skill at ~/.claude/skills/<kebab-name>/SKILL.md (or <cwd>/.claude/skills/ when the user says it belongs to this project only):

```
---
name: <kebab-name>
description: <when to follow this flow: the requests that should trigger it>
metadata:
  orla: flow
  title: <short title>
---

When a step begins, write a line "▶ <step title>". For several tasks at once, use one subagent per task, each in its own worktree, all following this flow.

## <Step title>
uses: <skills this step follows, comma separated>
signal: <tool call that shows the step is happening, e.g. Bash(git push*), Skill(name), mcp__server__tool>
branch: <another step> if <condition>
next: <step it goes to when done, or end>
file: steps/<step>.md

<what to do in this step>
```

Every line under a step title is optional; keep only those that apply.

- Steps follow the order written. A path taken only under a condition is a branch; its steps go after the main ones, and each says where it leads with `next:` (back to a main step, or `end`). A step that skips the ones below it says so with `next:` too. That way the main path and each conditional path read, and draw, apart.
- Write the commands a step runs in backticks (Run `git log --oneline -5`): Orla recognizes the step by them. Add `signal:` only for what is not a shell command (Skill(name), Read, mcp__server__tool). A step that runs nothing is recognized by its "▶" line.
- A convention several flows share (how a PR or a commit is written) becomes its own skill, named in `uses`. Reuse an existing skill in ~/.claude/skills when one fits.
- What must come out the same every time becomes a script in scripts/ that the step runs.
- Keep SKILL.md short; long instructions go in step files.
- Never invent details about the user's tools, company or repositories; ask.
- When done, say in one line that the flow is ready in the Flow tab.
