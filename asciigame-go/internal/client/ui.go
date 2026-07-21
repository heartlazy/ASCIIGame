package client

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/heartlazyli/asciigame/internal/config"
	"github.com/heartlazyli/asciigame/internal/protocol"
	"github.com/rivo/tview"
)

// UI wires the tview application: a login page and a game page with four
// panels (status/map/messages/help), replacing the C ncurses windows.
type UI struct {
	app   *tview.Application
	pages *tview.Pages
	state *State
	conn  *Conn
	addr  string

	status *tview.TextView
	world  *tview.TextView
	msgs   *tview.TextView
	help   *tview.TextView
	game   *tview.Flex

	username string
}

// NewUI builds the UI targeting server addr (host:port).
func NewUI(addr string) *UI {
	u := &UI{
		app:   tview.NewApplication(),
		pages: tview.NewPages(),
		state: NewState(),
		addr:  addr,
	}
	u.buildGamePage()
	u.buildLoginPage()
	u.pages.SwitchToPage("login")
	u.app.SetRoot(u.pages, true).EnableMouse(false)
	return u
}

// Run starts the event loop; blocks until quit.
func (u *UI) Run() error { return u.app.Run() }

func (u *UI) buildLoginPage() {
	var username, password string
	form := tview.NewForm()
	form.AddInputField("Username", "", 20, nil, func(t string) { username = t }).
		AddPasswordField("Password", "", 20, '*', func(t string) { password = t }).
		AddButton("Login", func() { u.connectAndAuth(username, password, false) }).
		AddButton("Register", func() { u.connectAndAuth(username, password, true) }).
		AddButton("Quit", func() { u.app.Stop() })
	form.SetBorder(true).SetTitle(" ASCII Battle Royale — Login ").SetTitleAlign(tview.AlignCenter)

	// Center the form in the terminal.
	flex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 11, 1, true).
			AddItem(nil, 0, 1, false), 40, 1, true).
		AddItem(nil, 0, 1, false)
	u.pages.AddPage("login", flex, true, true)
}

func (u *UI) buildGamePage() {
	u.status = tview.NewTextView().SetDynamicColors(true)
	u.status.SetBorder(true).SetTitle(" Status ")
	u.world = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	u.world.SetBorder(true).SetTitle(" Arena ")
	u.msgs = tview.NewTextView().SetDynamicColors(true).SetScrollable(true)
	u.msgs.SetBorder(true).SetTitle(" Messages ")
	u.help = tview.NewTextView().SetDynamicColors(true)

	u.game = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.status, 4, 0, false).
		AddItem(u.world, config.MapHeight+2, 0, false).
		AddItem(u.msgs, 0, 1, false).
		AddItem(u.help, 1, 0, false)
	u.game.SetInputCapture(u.onKey)
	u.pages.AddPage("game", u.game, true, false)
}

// connectAndAuth dials (once), starts the read loop, and sends REGISTER+LOGIN
// or just LOGIN, mirroring client/main.c's post-login-screen flow.
func (u *UI) connectAndAuth(username, password string, register bool) {
	if username == "" || password == "" {
		u.showModal("Username and password required.")
		return
	}
	if u.conn == nil {
		c, err := Dial(u.addr)
		if err != nil {
			u.showModal(fmt.Sprintf("Failed to connect: %v", err))
			return
		}
		u.conn = c
		go u.conn.ReadLoop(u.onMsg, u.onClose)
	}
	u.username = username
	u.state.mu.Lock()
	u.state.username = username
	u.state.loggedIn = true
	u.state.mu.Unlock()

	if register {
		_ = u.conn.Send(protocol.BuildRegister(username, password))
	}
	_ = u.conn.Send(protocol.BuildLogin(username, password))

	u.pages.SwitchToPage("game")
	u.app.SetFocus(u.game)
	u.render()
}

// onMsg runs on the read goroutine: update state, then redraw on the UI thread.
func (u *UI) onMsg(raw string) {
	if u.state.Update(raw) {
		u.app.QueueUpdateDraw(u.render)
	}
}

func (u *UI) onClose() {
	u.app.QueueUpdateDraw(func() {
		u.state.mu.Lock()
		u.state.addMessage("System", "Connection lost! Press Q to quit.")
		u.state.mu.Unlock()
		u.render()
	})
}

// onKey dispatches key presses by game phase, mirroring the C handle_*_input.
func (u *UI) onKey(ev *tcell.EventKey) *tcell.EventKey {
	u.state.mu.Lock()
	inGame, inRoom := u.state.inGame, u.state.inRoom
	u.state.mu.Unlock()

	r := ev.Rune()
	switch {
	case r == 'q' || r == 'Q':
		u.app.Stop()
		return nil
	}

	switch {
	case inGame:
		u.gameKey(ev)
	case inRoom:
		u.roomKey(ev)
	default:
		u.lobbyKey(ev)
	}
	return nil
}

func (u *UI) gameKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyUp:
		u.send(protocol.BuildMove('U'))
		return
	case tcell.KeyDown:
		u.send(protocol.BuildMove('D'))
		return
	case tcell.KeyLeft:
		u.send(protocol.BuildMove('L'))
		return
	case tcell.KeyRight:
		u.send(protocol.BuildMove('R'))
		return
	case tcell.KeyEnter:
		u.send(protocol.BuildAttack())
		return
	}
	switch ev.Rune() {
	case 'w', 'W':
		u.send(protocol.BuildMove('U'))
	case 's', 'S':
		u.send(protocol.BuildMove('D'))
	case 'a', 'A':
		u.send(protocol.BuildMove('L'))
	case 'd', 'D':
		u.send(protocol.BuildMove('R'))
	case 'j', 'J', ' ':
		u.send(protocol.BuildAttack())
	case '1', '2', '3', '4', '5':
		u.send(protocol.BuildUseItem(int(ev.Rune() - '1')))
	case 't', 'T':
		u.prompt("Chat", func(text string) {
			if strings.TrimSpace(text) != "" {
				u.send(protocol.BuildChat(text))
			}
		})
	}
}

func (u *UI) roomKey(ev *tcell.EventKey) {
	switch ev.Rune() {
	case 'r', 'R':
		u.send(protocol.BuildSimple("READY"))
	case 'l', 'L':
		u.send(protocol.BuildSimple("LEAVE_ROOM"))
	case 't', 'T':
		u.prompt("Chat", func(text string) {
			if strings.TrimSpace(text) != "" {
				u.send(protocol.BuildChat(text))
			}
		})
	}
}

func (u *UI) lobbyKey(ev *tcell.EventKey) {
	switch ev.Rune() {
	case 'c', 'C':
		u.send(protocol.BuildCreateRoom("Room", 6))
	case 'l', 'L':
		u.send(protocol.BuildSimple("LIST_ROOMS"))
	case 'j', 'J':
		u.prompt("Room ID", func(text string) {
			if id := atoi(text); id > 0 {
				u.send(protocol.BuildJoinRoom(id))
			}
		})
	}
}

func (u *UI) send(frame string) {
	if u.conn != nil {
		_ = u.conn.Send(frame)
	}
}

// prompt overlays a single-line input field and calls done with the entered
// text on Enter (Esc cancels).
func (u *UI) prompt(label string, done func(string)) {
	field := tview.NewInputField().SetLabel(label + ": ").SetFieldWidth(40)
	field.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			done(field.GetText())
		}
		u.pages.RemovePage("prompt")
		u.app.SetFocus(u.game)
	})
	field.SetBorder(true)
	modal := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(field, 3, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
		AddItem(nil, 0, 1, false)
	u.pages.AddPage("prompt", modal, true, true)
	u.app.SetFocus(field)
}

func (u *UI) showModal(text string) {
	m := tview.NewModal().SetText(text).AddButtons([]string{"OK"}).
		SetDoneFunc(func(int, string) {
			u.pages.RemovePage("modal")
		})
	u.pages.AddPage("modal", m, true, true)
}

// render rebuilds all panels from a state snapshot. Must run on the UI thread.
func (u *UI) render() {
	snap := u.state.Snapshot()
	u.renderStatus(&snap)
	u.renderWorld(&snap)
	u.renderMessages(snap.messages)
	u.renderHelp(&snap)
}

func (u *UI) renderStatus(s *snapshot) {
	roomLabel := "Lobby"
	if s.inRoom {
		roomLabel = fmt.Sprintf("%s (ID:%d)", s.roomName, s.roomID)
		if s.isReady {
			roomLabel += " [green]READY[-]"
		}
	}
	line1 := fmt.Sprintf("User: [yellow]%s[-]  |  Room: %s", s.username, roomLabel)
	var line2 string
	if s.inGame {
		shield := ""
		if s.myHasShield {
			shield = " [cyan]*SHIELD*[-]"
		}
		line2 = fmt.Sprintf("HP: %s  ATK: %d  DEF: %d  Pos:(%d,%d)  Inv:%s%s",
			hpBar(s.myHP, s.myMaxHP), s.myATK, s.myDEF, s.myX, s.myY, invString(s), shield)
	} else {
		line2 = "Not in game."
	}
	u.status.SetText(line1 + "\n" + line2)
}

func (u *UI) renderWorld(s *snapshot) {
	if !s.inGame {
		u.world.SetText("\n   Waiting for game to start...\n\n   Lobby: [C]reate room  [J]oin  [L]ist rooms  [Q]uit\n   Room:  [R]eady  [T]chat  [L]eave")
		return
	}
	// Base grid from the template.
	grid := make([][]rune, config.MapHeight)
	for y := 0; y < config.MapHeight; y++ {
		grid[y] = []rune(mapTemplate[y])
	}
	// Cell decorator: colored string per cell.
	cell := func(y, x int) string {
		c := grid[y][x]
		switch c {
		case '#':
			return "[white]#[-]"
		case '$', ' ':
			if mapIsInPoison(x, y, s.poisonRadius) {
				return "[red].[-]"
			}
			return " "
		default:
			return string(c)
		}
	}
	out := make([][]string, config.MapHeight)
	for y := 0; y < config.MapHeight; y++ {
		out[y] = make([]string, config.MapWidth)
		for x := 0; x < config.MapWidth; x++ {
			out[y][x] = cell(y, x)
		}
	}
	// Items overlay.
	for _, it := range s.items {
		if inBounds(it.x, it.y) {
			out[it.y][it.x] = "[yellow]" + string(itemChar(it.typ)) + "[-]"
		}
	}
	// Players overlay: self green, others (alive) white, dead dim.
	for _, p := range s.players {
		if !inBounds(p.x, p.y) {
			continue
		}
		color := "white"
		if p.id == s.myID {
			color = "green"
		}
		if p.status == 6 { // StatusDead
			color = "gray"
		}
		out[p.y][p.x] = "[" + color + "]@[-]"
	}

	var b strings.Builder
	for y := 0; y < config.MapHeight; y++ {
		for x := 0; x < config.MapWidth; x++ {
			b.WriteString(out[y][x])
		}
		b.WriteByte('\n')
	}
	u.world.SetText(b.String())
}

func (u *UI) renderMessages(msgs []chatMessage) {
	var b strings.Builder
	for _, m := range msgs {
		if m.sender == "" {
			b.WriteString(m.text)
		} else {
			b.WriteString("[teal][" + m.sender + "][-] " + tview.Escape(m.text))
		}
		b.WriteByte('\n')
	}
	u.msgs.SetText(b.String())
	u.msgs.ScrollToEnd()
}

func (u *UI) renderHelp(s *snapshot) {
	switch {
	case s.inGame:
		u.help.SetText("[white]WASD/↑↓←→[-] Move  [white]J/Space[-] Attack  [white]1-5[-] Item  [white]T[-] Chat  [white]Q[-] Quit")
	case s.inRoom:
		u.help.SetText("[white]R[-] Ready  [white]T[-] Chat  [white]L[-] Leave  [white]Q[-] Quit")
	default:
		u.help.SetText("[white]C[-] Create  [white]J[-] Join  [white]L[-] List rooms  [white]Q[-] Quit")
	}
}

// hpBar renders a colored HP indicator matching the C HP color tiers.
func hpBar(hp, max int) string {
	if max <= 0 {
		max = config.InitialHP
	}
	color := "green"
	switch {
	case hp*3 <= max:
		color = "red"
	case hp*3 <= max*2:
		color = "yellow"
	}
	return fmt.Sprintf("[%s]%d/%d[-]", color, hp, max)
}

func invString(s *snapshot) string {
	var parts []string
	for i := 0; i < config.MaxInventory; i++ {
		if s.inventory[i] > 0 {
			parts = append(parts, string(itemChar(s.inventory[i])))
		}
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, " ")
}

func itemChar(t int) byte {
	switch t {
	case 1:
		return '+'
	case 2:
		return '^'
	case 3:
		return '*'
	default:
		return '?'
	}
}

func inBounds(x, y int) bool {
	return x >= 0 && x < config.MapWidth && y >= 0 && y < config.MapHeight
}

// mapIsInPoison mirrors the server's Chebyshev-distance poison check.
func mapIsInPoison(x, y, radius int) bool {
	cx, cy := config.MapWidth/2, config.MapHeight/2
	dx, dy := x-cx, y-cy
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	dist := dx
	if dy > dx {
		dist = dy
	}
	return dist > radius
}
