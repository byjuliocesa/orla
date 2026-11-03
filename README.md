# Orla

A self-hosted workspace for AI agents. Coding agents such as Claude Code and Codex run in real
terminals on your server, with their steps and code changes visible from the browser, next to
persistent agents you can reach from chat channels.

Early development. Claude Code works today; Codex, Orla's own agent loop and chat channels are not
built yet.

## Layout

```text
cmd/orla/            The binary
internal/server/     HTTP API and WebSocket streams
internal/auth/       Access to the API
internal/events/     The common event format and its bus
internal/store/      SQLite persistence
internal/session/    Sessions and runs
internal/terminal/   PTYs
internal/git/        Worktrees, diffs, merges
internal/engine/cli/ Engine A: the official coding CLIs
internal/engine/loop/ Engine B: Orla's own agent loop
internal/provider/   Model provider APIs for engine B
internal/mcp/        MCP server the CLIs connect to
internal/channel/    Chat channels
internal/webui/      Serves the web client embedded in the binary
web/                 The web client: Next.js, exported as static files
```

## Running

```bash
make                     # builds the web client and the binary with it embedded (needs Node 22+)
./orla serve             # listens on 127.0.0.1:4410
```

`go build -o orla ./cmd/orla` builds the binary without the web client, for the command line
and the API alone.

The first run creates an access token in `~/.local/share/orla/token`. Clients on the same
machine read it from there; elsewhere, set `ORLA_TOKEN`. In the browser, open
`http://127.0.0.1:4410` and paste the token once: the browser keeps a login cookie for 30 days.
A proxy in front of Orla must pass the original `Host` header, which the browser checks are made
against.

To work on the web client, run `make web-dev` and `./orla serve -ui-dev http://127.0.0.1:3000`
in another shell: Orla then serves the client from the Next.js dev server, same origin as the API.

Terminals live on the server and keep running when you leave them:

```bash
./orla attach                    # new terminal with your shell, in the current folder
./orla attach -- claude          # new terminal running a command
# Ctrl-] detaches; the terminal keeps running
./orla ls                        # running terminals
./orla attach term_01...         # back in, with recent output replayed
./orla close term_01...          # end it and everything it started
```

Claude Code runs headless in sessions that remember the conversation, with each step shown as
it happens:

```bash
./orla run "find why the login test fails"         # new session in the current folder
./orla run -session ses_01... "now fix it"          # same conversation
./orla run -mode acceptEdits -model sonnet "..."    # permission mode and model for one run
./orla sessions
```

When Claude Code needs permission in a headless run, it asks you through Orla. `orla run` asks
in the terminal; any other device answers with `orla approve`, and `orla watch` shows the
request. Unanswered requests are denied after 10 minutes, and when the run is cancelled.

```bash
./orla run -mode manual "clean up the build folder"
./orla approve ses_01... apr_01...                     # allow
./orla approve -deny -m "keep build/" ses_01... apr_01...
```

Claude Code typed into an Orla terminal reports its steps too, once its hooks are installed:

```bash
./orla hooks install             # adds Orla's hooks to ~/.claude/settings.json, keeping the rest
./orla attach -- claude          # use it as usual
./orla watch                     # from anywhere: follow the latest session live
```

Its permission prompts reach you the same way: the terminal shows the prompt, `orla watch` shows
the request, and the first answer counts, typed in the terminal or sent with `orla approve`. A
request nobody answers through Orla is dropped after 10 minutes and the terminal keeps asking.

The hooks do nothing outside Orla terminals. Each terminal gets its own hook token, which only
reports that terminal's events; the owner token never enters a terminal. `orla hooks print`
shows the fragment instead, for `claude --settings`. Run `orla hooks install` again after
updating Orla, to pick up new hooks.

Ctrl-C cancels the run, and everything it started. Without `-mode`, Claude Code uses its own
default from `~/.claude/settings.json`, which may approve everything.

In the browser, a session in a git repository has a Changes tab: every file the agent changed,
with its diff, updated while it works. A new session can start in a worktree of its own, a branch
(`orla/<name>`) and a folder under `~/.local/share/orla/worktrees`, made from what the repository
has checked out. From the Changes tab you commit it, merge it back after a preview that names any
conflicting files, and remove it. Nothing is lost on the way: a discarded file and the
uncommitted work of a removed worktree go to a git stash, and a branch whose commits are not
merged stays. This needs git 2.38 or later.

To reach a server on another machine without exposing it, forward the port over SSH:
`ssh -L 4410:127.0.0.1:4410 your-server`.

## API

Every route under `/v1` needs `Authorization: Bearer <token>`, or the login cookie of a browser.
A request with the cookie that changes something must come from Orla's own pages (its `Origin`).

| Route | |
| --- | --- |
| `GET /health` | No token. Says the process is up. |
| `POST /auth/login` | No token. Exchanges `{"token": "..."}` for a login cookie. |
| `POST /auth/logout` | Ends the browser's login. |
| `GET /v1/terminals` | Running terminals. |
| `POST /v1/terminals` | Open one: `{"argv": [...], "cwd": "...", "cols": 80, "rows": 24}`, all optional. |
| `DELETE /v1/terminals/{id}` | End it and every process it started. |
| `GET /v1/sessions` | Agent sessions, each with its `state` (`working`, `waiting`, `idle`, `ended`), `pending` requests and `last_run`. |
| `POST /v1/sessions` | New session: `{"agent": "claude", "cwd": "..."}`. |
| `GET /v1/sessions/{id}` | A session and its runs. |
| `POST /v1/sessions/{id}/runs` | Start a run: `{"prompt": "...", "permission_mode": "...", "model": "..."}`. 409 while one is in progress. |
| `POST /v1/sessions/{id}/cancel` | Cancel the run in progress. |
| `GET /v1/sessions/{id}/events` | Server-Sent Events, from `?after=<seq>` or `Last-Event-ID`. |
| `GET /v1/events` | Server-Sent Events of every session, live only, for lists that follow them all. |
| `GET /v1/approvals` | Permission requests waiting for an answer, oldest first. |
| `POST /v1/sessions/{id}/approvals/{approval}` | Answer a permission request: `{"allow": true, "message": "...", "by": "web"}`. 404 once answered or expired. |
| `GET /v1/git?path=...` | Whether a folder is in a git repository: `{"git": true, "repo": {"root", "main", "branch", "head"}}`. |
| `POST /v1/worktrees` | New worktree: `{"from": "/path/in/repo", "branch": "optional"}`. Returns it and the `cwd` to start a session in. |
| `GET /v1/sessions/{id}/changes` | Files the session changed, against its worktree's base or the last commit, with `ahead` and `behind` for a worktree. |
| `GET /v1/sessions/{id}/diff?path=...` | The unified diff of one changed file, cut at 1 MB. |
| `POST /v1/sessions/{id}/discard` | Undo the uncommitted changes to one file, `{"path": "..."}`, keeping them in a stash. |
| `POST /v1/sessions/{id}/commit` | Commit every change in the session's work tree: `{"message": "..."}`. |
| `GET /v1/sessions/{id}/merge` | What merging the worktree's branch into its base would do: commits ahead and behind, conflicts. |
| `POST /v1/sessions/{id}/merge` | Merge it, in the work tree that has the base checked out. 409 with git's words when it cannot. |
| `DELETE /v1/sessions/{id}/worktree` | Remove the session's worktree; uncommitted work goes to a stash, an unmerged branch stays. |
| `GET /v1/terminals/{id}/stream` | WebSocket. Binary messages are terminal bytes both ways. Text messages are control: `{"type":"resize","cols":..,"rows":..}` from the client, `{"type":"exit","code":..}` from the server. |

## License

[MIT](LICENSE)
