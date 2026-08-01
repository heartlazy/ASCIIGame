# CLAUDE.md

## Project Overview

Go port of the ASCII Battle Royale multiplayer terminal game. Originally ~7900 lines of C (epoll+pthread+ncurses), now reimplemented in idiomatic Go.

Module: `github.com/heartlazyli/asciigame`

## Architecture

- **Server** (`cmd/server`): TCP listener, one goroutine per connection, one game-tick goroutine (50ms) per room. Shared state protected by `sync.RWMutex` (player/room registries) and per-object `sync.Mutex`.
- **Client** (`cmd/client`): tview TUI with 4 panels (status/map/messages/help). Network goroutine feeds state updates via `QueueUpdateDraw`.
- **Protocol** (`internal/protocol`): Wire-compatible text protocol `CMD|arg|...\n`. Golden-vector tests lock format.

## Key Commands

```bash
go build ./...                    # Build all
go test ./...                     # Unit + integration tests
go test -race ./...               # Race detector (Linux only, needs cgo)
go build -o bin/server ./cmd/server
go build -o bin/client ./cmd/client
```

## Wire Protocol (DO NOT CHANGE)

Format: `CMD|arg1|arg2|...\n` — fields separated by `|`, terminated by `\n`.
Parsing strips trailing `\r\n`, splits on `|`, **empty tokens are dropped** (strtok_r semantics).

GAME_STATE has 13-field player entries: `id,x,y,hp,atk,def,status,shield,inv0,inv1,inv2,inv3,inv4`

Conformance tests: `internal/protocol/protocol_test.go`

## Lock Ordering

Always: `room.mu` → `player.mu`. Registry locks (`pmu`, `rmu`) are leaf locks (never held while acquiring room/player locks). All network sends happen outside locks.

## Persistence

- **Accounts**: SQLite (`data/game.db`) via the pure-Go `modernc.org/sqlite`
  driver (no cgo). Passwords are bcrypt; legacy unsalted SHA-256 records (from
  the C server / old JSON store) still verify and upgrade to bcrypt on login. A
  legacy `data/users.json` is imported once on first startup, then renamed.
- **WAL**: text format `TS|SEQ|ROOM|ACTION|DATA\n` (C-compatible), fsync'd every 1s
- **Snapshot**: JSON, every 20s, atomic write (tmp+rename)
- **Recovery**: On startup, replays WAL for rooms without GAME_END; players rejoin on login

## Testing Against C Version

The Python tests in `../test/` work unchanged against the Go server:
```bash
PYTHONUTF8=1 python ../test/functional_test.py --port 8888
PYTHONUTF8=1 python ../test/robustness_test.py --port 8888
```
