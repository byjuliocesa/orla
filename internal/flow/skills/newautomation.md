---
name: newautomation
description: Create an Orla automation, a script Orla runs and watches so a routine job costs no model tokens.
disable-model-invocation: true
argument-hint: "[what it should do]"
metadata:
  orla: system
---

Create an Orla automation from what the user describes: $ARGUMENTS

If nothing was described, ask in one short message what it should do, when (always, every so often, or when something happens in Orla), and whether it belongs to one session or runs on its own. Then wait.

An automation is a folder ~/.claude/orla-automations/<kebab-name>/ with an AUTOMATION.md and its scripts:

```
---
name: <kebab-name>
description: <what it does, in one line>
run: python3 main.py
scope: session        # session: runs with one session and knows it; global: runs on its own
trigger: always       # always | every 5m | on approval.requested (a trailing * matches a family: run.*)
restart: on-failure   # on-failure | always | never (for "always")
metadata:
  title: <short title>
---

<a few lines on what it does and how to tell it is working>
```

What Orla gives the script, in its environment:
- ORLA_URL and ORLA_TOKEN: Orla's API; send `Authorization: Bearer $ORLA_TOKEN`.
- ORLA_SESSION and ORLA_CWD: the session and its folder (session scope).
- ORLA_EVENT: the event that ran it, as JSON (trigger "on"): {"type", "session", "run", "data", ...}.
- ORLA_AUTOMATION: its own name.

Orla's API, for the usual jobs:
- Events as they happen: GET $ORLA_URL/v1/events (Server-Sent Events, every session, only new ones; each `data:` line is one JSON event; keep those whose "session" is ORLA_SESSION). For a reaction to one kind of event, prefer trigger "on <type>" with ORLA_EVENT over a watcher.
- Waiting permission requests: GET $ORLA_URL/v1/approvals → {"approvals": [{"approval_id", "session", "tool", "input", ...}]}. An "approval.requested" event carries the same in its data.
- Answer one: POST $ORLA_URL/v1/sessions/<session>/approvals/<approval_id> with {"allow": true, "by": "auto:<name>"} ("by" at most 32 characters; {"allow": false, "message": "why"} refuses).

Rules:
- Python 3 standard library only (urllib, json), unless the user agrees to install something.
- Print one line for everything it does, and for every decision it skips: that is its log in Orla.
- Approve only what matches a narrow rule; never approve everything.
- Do not turn it on: the owner does, from Orla.

When done, say in one line that it is ready in the Automations tab, and how to turn it on.
