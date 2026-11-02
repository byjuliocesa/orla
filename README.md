# Orla

A self-hosted workspace for AI agents. Coding agents such as Claude Code and Codex run in real
terminals on your server, with their steps and code changes visible from the browser, next to
persistent agents you can reach from chat channels.

Early development: nothing usable yet. Start with [docs/architecture.md](docs/architecture.md).

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
docs/                Architecture, events, decisions
```

## Running

```bash
go build -o orla ./cmd/orla
./orla serve             # listens on 127.0.0.1:4410
```

The first run creates an access token in `~/.local/share/orla/token`. Clients on the same
machine read it from there; elsewhere, set `ORLA_TOKEN`.

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

The hooks do nothing outside Orla terminals. Each terminal gets its own hook token, which only
reports that terminal's events; the owner token never enters a terminal. `orla hooks print`
shows the fragment instead, for `claude --settings`.

Ctrl-C cancels the run, and everything it started. Without `-mode`, Claude Code uses its own
default from `~/.claude/settings.json`, which may approve everything.

To reach a server on another machine without exposing it, forward the port over SSH:
`ssh -L 4410:127.0.0.1:4410 your-server`.

## API

Every route under `/v1` needs `Authorization: Bearer <token>`.

| Route | |
| --- | --- |
| `GET /health` | No token. Says the process is up. |
| `GET /v1/terminals` | Running terminals. |
| `POST /v1/terminals` | Open one: `{"argv": [...], "cwd": "...", "cols": 80, "rows": 24}`, all optional. |
| `DELETE /v1/terminals/{id}` | End it and every process it started. |
| `GET /v1/sessions` | Agent sessions, newest first. |
| `POST /v1/sessions` | New session: `{"agent": "claude", "cwd": "..."}`. |
| `GET /v1/sessions/{id}` | A session and its runs. |
| `POST /v1/sessions/{id}/runs` | Start a run: `{"prompt": "...", "permission_mode": "...", "model": "..."}`. 409 while one is in progress. |
| `POST /v1/sessions/{id}/cancel` | Cancel the run in progress. |
| `GET /v1/sessions/{id}/events` | Server-Sent Events, from `?after=<seq>` or `Last-Event-ID`. See [docs/events.md](docs/events.md). |
| `POST /v1/sessions/{id}/approvals/{approval}` | Answer a permission request: `{"allow": true, "message": "...", "by": "web"}`. 404 once answered or expired. |
| `GET /v1/terminals/{id}/stream` | WebSocket. Binary messages are terminal bytes both ways. Text messages are control: `{"type":"resize","cols":..,"rows":..}` from the client, `{"type":"exit","code":..}` from the server. |

## License

[MIT](LICENSE)
