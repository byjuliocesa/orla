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
go run ./cmd/orla        # listens on 127.0.0.1:4410
curl 127.0.0.1:4410/health
```

## License

[MIT](LICENSE)
