# CLAUDE.md

## Project Overview

Go port of the ASCII Battle Royale multiplayer terminal game. Originally ~7900 lines of C (epoll+pthread+ncurses), now reimplemented in idiomatic Go.

Module: `github.com/heartlazyli/asciigame`

## Architecture

- **Server** (`cmd/server`): TCP listener, one goroutine per connection, one game-tick goroutine (50ms) per room. Shared state protected by `sync.RWMutex` (player/room registries) and per-object `sync.Mutex`.
- **Client** (`cmd/client`): tview TUI with 4 panels (status/map/messages/help). Network goroutine feeds state updates via `QueueUpdateDraw`.
- **Protocol** (`internal/protocol`): protobuf messages (generated from `asciigame.proto`) with length-prefix framing. A `Frame` carries a `oneof payload` discriminating all message types; `GameEvent` has a nested `oneof event`.

## Key Commands

```bash
go build ./...                    # Build all
go test ./...                     # Unit + integration tests
go test -race ./...               # Race detector (Linux only, needs cgo)
go build -o bin/server ./cmd/server
go build -o bin/client ./cmd/client
```

Regenerating protobuf code (only when `internal/protocol/asciigame.proto` changes — the generated `asciigame.pb.go` is committed):
```bash
protoc --proto_path=internal/protocol \
  --go_out=internal/protocol --go_opt=paths=source_relative \
  internal/protocol/asciigame.proto
```

## Wire Protocol

Each frame on the wire is **4-byte big-endian length + marshaled protobuf `Frame`** (see `codec.go`). The `Frame.payload` oneof discriminates the message; `GameEvent.event` is a nested oneof for attack/damage/kill/shield/pickup/poison/buff events. Typed accessors (`f.GetOk()`, `f.GetGameState()`, …) read payloads safely.

Round-trip tests: `internal/protocol/codec_test.go`. Note: this is **not** byte-compatible with the original C text protocol — the old Python tests in `../test/` are obsolete and need rewriting against the new binary protocol.

## Layer-1 optimization: dirty detection

`broadcastState` skips the periodic GAME_STATE broadcast when the observable state (players/items/poison, excluding the ever-changing timestamp) is byte-identical to the last broadcast. Idle periods no longer flood clients at 20 Hz; any real change (move, damage, pickup, poison shrink, join/leave) alters the signature and sends. Discrete `GameEvent` frames are unaffected.

## Lock Ordering

Always: `room.mu` → `player.mu`. Registry locks (`pmu`, `rmu`) are leaf locks (never held while acquiring room/player locks). All network sends happen outside locks.

## Persistence

- **Accounts**: SQLite (`data/game.db`) via the pure-Go `modernc.org/sqlite`
  driver (no cgo). Passwords are bcrypt; legacy unsalted SHA-256 records (from
  the C server / old JSON store) still verify and upgrade to bcrypt on login. A
  legacy `data/users.json` is imported once on first startup, then renamed.
- **WAL**: text format `TS|SEQ|ROOM|ACTION|DATA\n` (C-compatible), fsync'd every 1s. Persistence is deliberately separate from the network protocol.
- **Snapshot**: JSON, every 20s, atomic write (tmp+rename)
- **Recovery**: On startup, replays WAL for rooms without GAME_END; players rejoin on login
