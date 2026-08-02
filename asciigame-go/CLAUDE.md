# CLAUDE.md

## Project Overview

Go port of the ASCII Battle Royale multiplayer terminal game. Dual-protocol architecture: HTTP API (Gin) for lobby/room operations, TCP (protobuf) for real-time game I/O.

Module: `github.com/heartlazyli/asciigame`

## Architecture

**Two servers run in parallel:**
- **HTTP API** (`:8080`, Gin): register, login (returns token), create/join/list/leave room, ready, chat, logout
- **TCP Server** (`:8888`, protobuf + 4B length-prefix): real-time game frames only (Auth, Move, Attack, UseItem; server pushes GameStart/GameState/GameEvent/GameEnd/ChatMsg/Kick/PlayerJoin/PlayerLeave)

**Flow:**
1. Client calls `POST /api/login` → gets token + player_id
2. Client TCP connects → sends `Auth{token}` → server validates, binds Player
3. Client calls `POST /api/rooms/ready` → if allReady, game starts → TCP pushes `GameStart`
4. Game: client sends Move/Attack/UseItem via TCP; server pushes state at 20Hz
5. Game ends → TCP pushes `GameEnd` → client calls HTTP to leave/rejoin

**Server internals:**
- One goroutine per TCP connection, one game-tick goroutine (50ms) per room
- Shared state: `sync.RWMutex` (player/room registries) + per-object `sync.Mutex`
- Lock ordering: `room.mu` → `player.mu`; registry locks are leaf locks
- Dirty detection: `broadcastState` skips identical periodic GAME_STATE frames

## Key Commands

```bash
go build ./...                    # Build all
go test ./...                     # Unit + integration tests
go test -race ./...               # Race detector (Linux only, needs cgo)
go build -o bin/server ./cmd/server
go build -o bin/client ./cmd/client

# Run server (TCP :8888, HTTP :8080 by default)
./bin/server [tcp_port] [http_port]

# Run client
./bin/client [host] [tcp_port] [http_port]
```

## HTTP API Endpoints

All authenticated endpoints require `Authorization: Bearer <token>` header.

| Method | Path | Auth | Body | Response |
|--------|------|------|------|----------|
| POST | /api/register | No | {username, password} | {message} |
| POST | /api/login | No | {username, password} | {token, player_id} |
| GET | /api/rooms | Yes | — | [{room_id, name, ...}] |
| POST | /api/rooms | Yes | {name, max_players} | {room_id, ...} |
| POST | /api/rooms/:id/join | Yes | — | {room_id, ...} |
| POST | /api/rooms/leave | Yes | — | {message} |
| POST | /api/rooms/ready | Yes | — | {message, game_started} |
| POST | /api/chat | Yes | {message} | {message: "sent"} |
| POST | /api/logout | Yes | — | {message} |

## TCP Protocol

Wire format: `[4-byte big-endian uint32 length][protobuf Frame]`

Client sends: `Auth{token}` (first frame), then `Move{direction}` / `Attack{}` / `UseItem{index}`.

Server pushes: `Ok` / `Error` / `GameStart` / `GameState` / `GameEvent` (oneof: Attack/Damage/Kill/Shield/AttackResult/Pickup/Poison/BuffWarning/BuffExpired) / `GameEnd` / `ChatMsg` / `Kick` / `PlayerJoin` / `PlayerLeave` / `RoomInfo`.

Schema: `internal/protocol/asciigame.proto`

## Persistence

- **Accounts**: SQLite (`data/game.db`), pure-Go driver, bcrypt passwords
- **WAL**: text `TS|SEQ|ROOM|ACTION|DATA\n`, fsync'd every 1s
- **Snapshot**: JSON, every 20s, atomic write
- **Recovery**: startup replays WAL for rooms without GAME_END; TCP auth triggers rejoin
