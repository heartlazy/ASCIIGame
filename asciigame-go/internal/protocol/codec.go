// Package protocol is the wire layer: protobuf messages (generated in
// asciigame.pb.go) plus length-prefixed framing and convenience constructors.
//
// On the wire each Frame is encoded as:
//
//	[4-byte big-endian uint32 length][length bytes of marshaled Frame]
//
// protobuf gives compact, typed, schema-versioned payloads; the length prefix
// lets a single TCP read recover message boundaries (replacing the old
// newline-delimited text protocol).
package protocol

import (
	"encoding/binary"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"
)

// MaxFrameSize bounds a single frame to protect against malformed/truncated
// length headers. A real GAME_STATE is well under 1 KiB even with 10 players.
const MaxFrameSize = 1 << 20 // 1 MiB

// WriteFrame marshals f and writes a length-prefixed frame to w.
func WriteFrame(w io.Writer, f *Frame) error {
	data, err := proto.Marshal(f)
	if err != nil {
		return err
	}
	if len(data) > MaxFrameSize {
		return fmt.Errorf("protocol: frame too large (%d bytes)", len(data))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ReadFrame reads one length-prefixed frame from r.
func ReadFrame(r io.Reader) (*Frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		return &Frame{}, nil
	}
	if n > MaxFrameSize {
		return nil, fmt.Errorf("protocol: frame too large (%d bytes)", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	f := &Frame{}
	if err := proto.Unmarshal(buf, f); err != nil {
		return nil, err
	}
	return f, nil
}

// ---- TCP Frame constructors: only messages that travel as protobuf frames. ----

// Client -> Server (TCP)
func NewAuth(token string) *Frame { return &Frame{Payload: &Frame_Auth{Auth: &Auth{Token: token}}} }
func NewMove(direction string) *Frame {
	return &Frame{Payload: &Frame_Move{Move: &Move{Direction: direction}}}
}
func NewAttack() *Frame            { return &Frame{Payload: &Frame_Attack{Attack: &Attack{}}} }
func NewUseItem(index int32) *Frame {
	return &Frame{Payload: &Frame_UseItem{UseItem: &UseItem{Index: index}}}
}

// Server -> Client (TCP push)
func NewOk(message string, playerID int32) *Frame {
	return &Frame{Payload: &Frame_Ok{Ok: &Ok{Message: message, PlayerId: playerID}}}
}

func NewError(code int32, message string) *Frame {
	return &Frame{Payload: &Frame_Error{Error: &Error{Code: code, Message: message}}}
}

func NewRoomList(rooms []*RoomInfo) *Frame {
	return &Frame{Payload: &Frame_RoomList{RoomList: &RoomList{Rooms: rooms}}}
}

func NewRoomInfo(roomID int32, name string, playerCount, maxPlayers, status int32) *Frame {
	return &Frame{Payload: &Frame_RoomInfo{RoomInfo: &RoomInfo{
		RoomId: roomID, Name: name, PlayerCount: playerCount, MaxPlayers: maxPlayers, Status: status,
	}}}
}

func NewPlayerJoin(playerID int32, username string) *Frame {
	return &Frame{Payload: &Frame_PlayerJoin{PlayerJoin: &PlayerJoin{PlayerId: playerID, Username: username}}}
}

func NewPlayerLeave(playerID int32) *Frame {
	return &Frame{Payload: &Frame_PlayerLeave{PlayerLeave: &PlayerLeave{PlayerId: playerID}}}
}

func NewMapData(rows []string) *Frame {
	return &Frame{Payload: &Frame_MapData{MapData: &MapData{Rows: rows}}}
}

func NewGameState(timestamp int64, players []*PlayerState, items []*ItemState, poisonRadius int32) *Frame {
	return &Frame{Payload: &Frame_GameState{GameState: &GameState{
		Timestamp: timestamp, Players: players, Items: items, PoisonRadius: poisonRadius,
	}}}
}

func NewGameEnd(winnerID int32, stats string) *Frame {
	return &Frame{Payload: &Frame_GameEnd{GameEnd: &GameEnd{WinnerId: winnerID, Stats: stats}}}
}

func NewChatMsg(sender, message string) *Frame {
	return &Frame{Payload: &Frame_ChatMsg{ChatMsg: &ChatMsg{Sender: sender, Message: message}}}
}

func NewKick(reason string) *Frame {
	return &Frame{Payload: &Frame_Kick{Kick: &Kick{Reason: reason}}}
}

// ---- GameEvent constructors (each wraps a GameEvent with its oneof kind). ----

func NewAttackEvent(attackerID, x, y int32) *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_Attack{
		Attack: &AttackEvent{AttackerId: attackerID, X: x, Y: y},
	}}}}
}

func NewDamageEvent(attackerID, victimID, damage, hp int32) *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_Damage{
		Damage: &DamageEvent{AttackerId: attackerID, VictimId: victimID, Damage: damage, Hp: hp},
	}}}}
}

func NewKillEvent(killerID, victimID int32) *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_Kill{
		Kill: &KillEvent{KillerId: killerID, VictimId: victimID},
	}}}}
}

func NewShieldEvent(attackerID, defenderID int32) *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_Shield{
		Shield: &ShieldEvent{AttackerId: attackerID, DefenderId: defenderID},
	}}}}
}

func NewAttackResultEvent(attackerID, hitCount int32) *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_AttackResult{
		AttackResult: &AttackResultEvent{AttackerId: attackerID, HitCount: hitCount},
	}}}}
}

func NewPickupEvent(playerID, itemType int32) *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_Pickup{
		Pickup: &PickupEvent{PlayerId: playerID, ItemType: itemType},
	}}}}
}

func NewPoisonEvent() *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_Poison{
		Poison: &PoisonEvent{},
	}}}}
}

func NewBuffWarningEvent(playerID, seconds int32) *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_BuffWarning{
		BuffWarning: &BuffWarningEvent{PlayerId: playerID, Seconds: seconds},
	}}}}
}

func NewBuffExpiredEvent(playerID int32) *Frame {
	return &Frame{Payload: &Frame_GameEvent{GameEvent: &GameEvent{Event: &GameEvent_BuffExpired{
		BuffExpired: &BuffExpiredEvent{PlayerId: playerID},
	}}}}
}
