---
name: orla
description: What Orla is and what it does - sessions, approvals, worktrees, the library (skills, flows, automations, MCP servers, environments) and its sync. Use when the user asks about Orla, where something is set up in it, why something behaves as it does in Orla, or when a task depends on one of its features.
metadata:
  orla: system
---

# Orla

Orla is a self-hosted workspace for coding agents. Claude Code runs on the owner's machine, and
Orla shows each session in the browser: its steps as they happen, the files it changed, the
permission requests it is waiting on. It serves on `127.0.0.1:4410`, and is reached from another
machine through an SSH tunnel. Its data lives in `~/.local/share/orla`.

You may be running inside an Orla session right now: `ORLA_RUN` in your environment says so.

## Sessions

- A session is one conversation with the agent, in one folder. Each message is a run, and each
  run starts Claude Code afresh, resuming the conversation: a new MCP server, skill, setting or
  Claude version applies from the next message, with no restart.
- A permission prompt reaches the owner in Orla, in the browser or with `orla approve`. A request
  nobody answers is denied after 10 minutes.
- A session can start in a worktree of its own: a branch `orla/<name>` in a folder under
  `~/.local/share/orla/worktrees`. Its Changes tab commits, merges back and removes it. Discarded
  work goes to a git stash, never lost.
- Terminals live on the server and keep running when the browser leaves.

## The agent

On first visit, Orla checks that Claude Code is installed and signed in, and can install it and
sign it in from the browser. Orla never sees the account's credentials.

## The library

The owner's library is one git repository, synced to every machine that runs Orla: changes are
pushed about 15 seconds after they settle, and other machines pull every 5 minutes. The chip in
the Library header shows its state, and its menu syncs now, turns auto-sync off or stops syncing.
It lives in `~/.local/share/orla/library`:

| Folder | Holds |
| --- | --- |
| `shared/` | Skills every agent loads, linked into `~/.claude/skills` |
| `claude/`, `codex/` | Skills only that agent loads |
| `automations/` | Orla's automations |
| `sessions/` | Conversations saved to open on another machine |
| `mcp/` | MCP servers; their keys sealed in `secrets.age` |
| `env/` | Environments; their values sealed in the same file |

Library tabs:

- **Skills**: what the library holds and what is only on this machine. *Add to library* moves a
  skill in; *Remove* takes it out and leaves it on this machine only.
- **Flows**: skills marked `metadata: orla: flow`, the steps for a kind of task. A session
  follows the flows it picks. `/newflow`, `/editflow`, `/archiveflow`, `/deleteflow`.
- **Automations**: scripts Orla runs and watches, costing no tokens. `/newautomation`,
  `/editautomation`.
- **MCP**: servers the agent reaches. Orla writes each one into Claude's user config
  (`claude mcp add-json -s user`) and checks it, showing how many tools it lists or why it
  failed. A bare token in an `Authorization` header gets `Bearer` in front. *Move in* brings a
  server Claude already has into the library, key and all.
- **Environments**: named sets of variables, each linked to repositories by `host/owner/name`
  and a file (`.env` by default). Before and after each run, Orla writes the file into every
  checkout of a linked repository it finds: the session's folder and the repositories up to two
  levels below it, so a repository cloned during a run has its file by the next message. It
  keeps the file out of git through `.git/info/exclude`. Several
  environments for one file merge; where they share a key, the one linked to fewer repositories
  wins. A file Orla did not write (it starts with Orla's own comment line) is never overwritten.

## Keys and secrets

- The library's SSH key, in `~/.local/share/orla/ssh/key`, is pasted or made in the onboarding.
  Orla also hands it to everything it starts (runs, terminals, automations) through
  `GIT_SSH_COMMAND`, so one key serves the library and the repositories. It is never copied to
  `~/.ssh`, and a `GIT_SSH_COMMAND` the owner set is left alone.
- MCP keys and environment values travel encrypted with age to every machine's SSH key listed in
  `mcp/machines`. A new machine can read them once a machine that already can syncs.
- On the machine itself they are in the clear where they are used: MCP keys in
  `~/.claude.json`, environment values in the written `.env`. The encryption covers the copy that
  travels.

## When the owner asks for something Orla does not do yet

Say so plainly and name the closest thing it does. Do not invent settings, routes or menus.
