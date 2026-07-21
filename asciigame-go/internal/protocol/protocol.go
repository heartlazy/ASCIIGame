// Package protocol implements the wire protocol, ported verbatim from the C
// version's common/protocol.{c,h}. The wire format MUST stay byte-for-byte
// identical to the C implementation so the existing Python black-box tests
// (test/*.py) keep passing against the Go server.
//
// Frame format: CMD|arg1|arg2|...\n
//   - fields separated by '|'
//   - terminated by '\n'
//   - parsing strips trailing \r\n, then splits on '|' with strtok_r
//     semantics: empty tokens are skipped (see Parse).
package protocol

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/heartlazyli/asciigame/internal/config"
)

// Command enumerates every message type. The iota order matches the C enum in
// protocol.h:15-43 exactly (LOGIN=0 ... KICK=24, UNKNOWN=25).
type Command int

const (
	CmdLogin Command = iota
	CmdRegister
	CmdListRooms
	CmdJoinRoom
	CmdCreateRoom
	CmdLeaveRoom
	CmdReady
	CmdMove
	CmdAttack
	CmdUseItem
	CmdChat
	CmdLogout
	CmdOK
	CmdErr
	CmdRoomList
	CmdRoomInfo
	CmdPlayerJoin
	CmdPlayerLeave
	CmdGameStart
	CmdMapData
	CmdGameState
	CmdGameEvent
	CmdGameEnd
	CmdChatMsg
	CmdKick
	CmdUnknown
)

// cmdNames maps a Command to its wire name. Index == Command value.
var cmdNames = [...]string{
	"LOGIN", "REGISTER", "LIST_ROOMS", "JOIN_ROOM", "CREATE_ROOM",
	"LEAVE_ROOM", "READY", "MOVE", "ATTACK", "USE_ITEM", "CHAT", "LOGOUT",
	"OK", "ERR", "ROOM_LIST", "ROOM_INFO", "PLAYER_JOIN", "PLAYER_LEAVE",
	"GAME_START", "MAP_DATA", "GAME_STATE", "GAME_EVENT", "GAME_END",
	"CHAT_MSG", "KICK", "UNKNOWN",
}

var nameToCmd = func() map[string]Command {
	m := make(map[string]Command, len(cmdNames))
	for i, n := range cmdNames {
		m[n] = Command(i)
	}
	return m
}()

// CmdName returns the wire name for a Command, "UNKNOWN" if out of range.
func CmdName(c Command) string {
	if c < 0 || int(c) >= len(cmdNames) {
		return "UNKNOWN"
	}
	return cmdNames[c]
}

// ParseCmd maps a command string to its Command, CmdUnknown if unrecognized.
// Mirrors protocol_parse_cmd: "UNKNOWN" itself is not matched (loop excludes
// the last entry), so it also returns CmdUnknown.
func ParseCmd(s string) Command {
	if c, ok := nameToCmd[s]; ok && c != CmdUnknown {
		return c
	}
	return CmdUnknown
}

// Message is a parsed protocol message.
type Message struct {
	Type Command
	Args []string
}

// Parse decodes a raw frame into a Message, mirroring protocol_parse
// (protocol.c:71-123).
//
// Semantics replicated from the C strtok_r-based parser:
//   - trailing '\n'/'\r' are stripped,
//   - the string is split on '|' with empty tokens skipped (strtok_r collapses
//     consecutive delimiters and ignores leading/trailing ones),
//   - the first token is the command; remaining tokens are args, capped at
//     MaxArgs and truncated to MaxArgLen-1 bytes each.
//
// Returns an error for a nil/empty input or one with no tokens, matching the C
// return of -1.
func Parse(raw string) (Message, error) {
	if raw == "" {
		return Message{Type: CmdUnknown}, fmt.Errorf("protocol: empty message")
	}
	if len(raw) >= config.MaxMsgLen {
		return Message{Type: CmdUnknown}, fmt.Errorf("protocol: message too long")
	}

	// Strip trailing CR/LF.
	s := strings.TrimRight(raw, "\r\n")

	// strtok_r semantics: split on '|', dropping empty fields.
	tokens := strings.FieldsFunc(s, func(r rune) bool { return r == '|' })
	if len(tokens) == 0 {
		return Message{Type: CmdUnknown}, fmt.Errorf("protocol: no command token")
	}

	msg := Message{Type: ParseCmd(tokens[0])}
	for _, tok := range tokens[1:] {
		if len(msg.Args) >= config.MaxArgs {
			break
		}
		if len(tok) > config.MaxArgLen-1 {
			tok = tok[:config.MaxArgLen-1]
		}
		msg.Args = append(msg.Args, tok)
	}
	return msg, nil
}

// ---- Builders (server -> client). Each returns the full frame incl. '\n'. ----

func BuildOK(message string) string { return "OK|" + message + "\n" }

func BuildErr(code int, message string) string {
	return "ERR|" + strconv.Itoa(code) + "|" + message + "\n"
}

func BuildRoomList(roomData string) string { return "ROOM_LIST|" + roomData + "\n" }

func BuildRoomInfo(roomID int, name string, playerCount, maxPlayers, status int) string {
	return fmt.Sprintf("ROOM_INFO|%d|%s|%d|%d|%d\n", roomID, name, playerCount, maxPlayers, status)
}

func BuildPlayerJoin(playerID int, username string) string {
	return fmt.Sprintf("PLAYER_JOIN|%d|%s\n", playerID, username)
}

func BuildPlayerLeave(playerID int) string {
	return fmt.Sprintf("PLAYER_LEAVE|%d\n", playerID)
}

func BuildGameStart() string { return "GAME_START\n" }

// BuildMapData encodes the 20x50 map, mirroring protocol_build_map_data
// (protocol.c:224-249): rows joined by ',', each cell copied as-is with '\0'
// rendered as a space.
func BuildMapData(m *[config.MapHeight][config.MapWidth + 1]byte) string {
	var b strings.Builder
	b.WriteString("MAP_DATA|")
	for y := 0; y < config.MapHeight; y++ {
		if y > 0 {
			b.WriteByte(',')
		}
		for x := 0; x < config.MapWidth; x++ {
			c := m[y][x]
			if c == 0 {
				c = ' '
			}
			b.WriteByte(c)
		}
	}
	b.WriteByte('\n')
	return b.String()
}

// BuildGameState mirrors protocol_build_game_state (protocol.c:251-262).
// players/items are pre-assembled by the caller (see game.go); this only frames
// them. Player entries carry 13 fields: id,x,y,hp,atk,def,status,shield,inv0..4.
func BuildGameState(timestamp int64, playerStates, itemStates string, poisonRadius int) string {
	return fmt.Sprintf("GAME_STATE|%d|%s|%s|%d\n", timestamp, playerStates, itemStates, poisonRadius)
}

func BuildGameEvent(eventType, data string) string {
	return "GAME_EVENT|" + eventType + "|" + data + "\n"
}

func BuildGameEnd(winnerID int, stats string) string {
	return fmt.Sprintf("GAME_END|%d|%s\n", winnerID, stats)
}

func BuildChatMsg(sender, message string) string {
	return "CHAT_MSG|" + sender + "|" + message + "\n"
}

func BuildKick(reason string) string { return "KICK|" + reason + "\n" }

// ---- Builders (client -> server). Used by the Go client. ----

func BuildLogin(username, password string) string {
	return "LOGIN|" + username + "|" + password + "\n"
}

func BuildRegister(username, password string) string {
	return "REGISTER|" + username + "|" + password + "\n"
}

func BuildCreateRoom(roomName string, maxPlayers int) string {
	return fmt.Sprintf("CREATE_ROOM|%s|%d\n", roomName, maxPlayers)
}

func BuildJoinRoom(roomID int) string {
	return fmt.Sprintf("JOIN_ROOM|%d\n", roomID)
}

func BuildMove(direction byte) string { return "MOVE|" + string(direction) + "\n" }

func BuildAttack() string { return "ATTACK|\n" }

func BuildUseItem(itemIndex int) string {
	return fmt.Sprintf("USE_ITEM|%d\n", itemIndex)
}

func BuildChat(message string) string { return "CHAT|" + message + "\n" }

// BuildSimple frames a no-arg command as "CMD|\n" (protocol_build_simple),
// used for LIST_ROOMS, LEAVE_ROOM, READY, LOGOUT.
func BuildSimple(cmd string) string { return cmd + "|\n" }
