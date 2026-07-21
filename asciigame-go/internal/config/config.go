// Package config holds all global constants ported verbatim from the C
// version's common/config.h. Values must not drift from the C constants —
// the wire protocol and game rules depend on them.
package config

// Server
const (
	ServerPort     = 8888
	MaxEvents      = 64
	RecvBufferSize = 4096
	SendBufferSize = 4096
)

// Player
const (
	MaxPlayers   = 256
	MaxUsername  = 32
	MaxPassword  = 64
	MaxInventory = 5
)

// Room
const (
	MaxRooms           = 32
	MaxRoomPlayers     = 10
	MinRoomPlayers     = 2
	MaxRoomName        = 32
	GameStartCountdown = 3000 // ms
)

// Map
const (
	MapWidth    = 50
	MapHeight   = 20
	MaxMapItems = 20
)

// Game
const (
	TickIntervalMS   = 50 // 20 tick/s
	MoveCooldownMS   = 200
	AttackCooldownMS = 1000
	AttackRange      = 3 // Manhattan distance

	InitialHP  = 100
	InitialATK = 15
	InitialDEF = 5

	PoisonStartTime      = 60000 // ms, poison starts shrinking after 60s
	PoisonShrinkInterval = 30000 // ms, shrink every 30s
	PoisonDamage         = 5     // per second

	ItemSpawnInterval = 10000  // ms
	GameMaxDuration   = 300000 // ms, 5 min

	HealthRestore   = 30
	AtkBuffAmount   = 10
	AtkBuffDuration = 10000 // ms
)

// Protocol
const (
	MaxArgs           = 16
	MaxArgLen         = 2048
	MaxMsgLen         = 4096
	ProtocolDelimiter = '|'
	ProtocolTerminator = '\n'
)

// WAL
const (
	WalDir            = "data/wal"
	WalBufferSize     = 4096
	WalSyncIntervalMS = 1000
	WalMaxRetry       = 3
	RecoveryWaitTime  = 30000 // ms
)

// Snapshot
const (
	SnapshotDir        = "data/wal"
	SnapshotIntervalMS = 20000 // ms
)

// Storage
const (
	UsersFile       = "data/users.json" // Go version uses JSON (C used data/users.dat)
	MaxUsers        = 10240
	PasswordHashLen = 65 // SHA256 hex + null
)

// Error codes (must match C for Python test compatibility).
const (
	// Protocol errors 1001-1099
	ErrInvalidFormat   = 1001
	ErrUnknownCommand  = 1002
	ErrInvalidArgCount = 1003
	ErrInvalidArgFormat = 1004

	// Auth errors 2001-2099
	ErrUsernameExists     = 2001
	ErrInvalidCredentials = 2002
	ErrUserLoggedIn       = 2003

	// Room errors 3001-3099
	ErrRoomNotFound    = 3001
	ErrRoomFull        = 3002
	ErrGameInProgress  = 3003
	ErrNotInRoom       = 3004

	// Game errors 4001-4099
	ErrMoveCooldown     = 4001
	ErrInvalidMove      = 4002
	ErrAttackCooldown   = 4003
	ErrInvalidItemIndex = 4004
)
