package client

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

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

	username  string
	connected atomic.Bool
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
		AddButton("Login", func() { u.doLogin(username, password) }).
		AddButton("Register+Login", func() { u.doRegisterAndLogin(username, password) }).
		AddButton("Quit", func() { u.app.Stop() })
	form.SetBorder(true).SetTitle(" ASCII Battle Royale ").SetTitleAlign(tview.AlignCenter)

	// Give the form enough height for 2 fields + 3 buttons + border + title.
	flex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(form, 13, 1, true).
			AddItem(nil, 0, 1, false), 50, 1, true).
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
		AddItem(u.world, 8, 0, false). // starts in lobby; render() grows it to 22 in game
		AddItem(u.msgs, 0, 1, false).
		AddItem(u.help, 1, 0, false)
	u.pages.AddPage("game", u.game, true, false)

	// Capture input at the app level so keys work regardless of which child
	// widget has focus within the game page.
	u.app.SetInputCapture(func(ev *tcell.EventKey) *tcell.EventKey {
		// Only intercept when on the game page (not login, prompt, or modal).
		name, _ := u.pages.GetFrontPage()
		debugf("app input: frontPage=%q rune=%q key=%v", name, ev.Rune(), ev.Key())
		if name != "game" {
			return ev
		}
		return u.onKey(ev)
	})
}

// ensureConn establishes the TCP connection (once) and starts the read loop.
func (u *UI) ensureConn() error {
	if u.conn != nil {
		return nil
	}
	c, err := Dial(u.addr)
	if err != nil {
		return err
	}
	u.conn = c
	u.connected.Store(true)
	go u.conn.ReadLoop(u.onMsg, u.onClose)
	return nil
}

// doLogin connects and sends LOGIN, then waits for server response before
// transitioning to the game page. Shows an error modal on failure.
func (u *UI) doLogin(username, password string) {
	if username == "" || password == "" {
		u.showModal("Username and password required.")
		return
	}
	if err := u.ensureConn(); err != nil {
		u.showModal(fmt.Sprintf("Failed to connect: %v", err))
		return
	}
	_ = u.conn.Send(protocol.BuildLogin(username, password))

	// Wait briefly for the server response (the read goroutine updates state).
	u.username = username
	u.state.mu.Lock()
	u.state.username = username
	u.state.mu.Unlock()

	go u.waitForLoginResult(username)
}

// doRegisterAndLogin sends REGISTER, waits for success, then sends LOGIN.
func (u *UI) doRegisterAndLogin(username, password string) {
	if username == "" || password == "" {
		u.showModal("Username and password required.")
		return
	}
	if err := u.ensureConn(); err != nil {
		u.showModal(fmt.Sprintf("Failed to connect: %v", err))
		return
	}
	u.username = username
	u.state.mu.Lock()
	u.state.username = username
	u.state.mu.Unlock()

	_ = u.conn.Send(protocol.BuildRegister(username, password))

	// Wait for register response, then login.
	go func() {
		if !u.waitForResponse("Registration successful", "Register failed") {
			return
		}
		_ = u.conn.Send(protocol.BuildLogin(username, password))
		u.waitForLoginResult(username)
	}()
}

// waitForLoginResult polls state for up to 3s to see if login succeeded (myID
// was set by the OK handler), or if an error message appeared.
func (u *UI) waitForLoginResult(username string) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		u.state.mu.Lock()
		id := u.state.myID
		msgs := u.state.messages
		u.state.mu.Unlock()
		if id > 0 {
			// Login succeeded — switch to game page on the UI thread.
			u.state.mu.Lock()
			u.state.loggedIn = true
			u.state.mu.Unlock()
			u.app.QueueUpdateDraw(func() {
				u.pages.SwitchToPage("game")
				u.app.SetFocus(u.game)
				u.render()
			})
			return
		}
		// Check if an error appeared.
		if len(msgs) > 0 {
			last := msgs[len(msgs)-1]
			if last.sender == "Error" {
				u.app.QueueUpdateDraw(func() {
					u.showModal("Login failed: " + last.text)
				})
				return
			}
		}
	}
	u.app.QueueUpdateDraw(func() {
		u.showModal("Login timed out — no server response.")
	})
}

// waitForResponse polls state for up to 3s for a message containing success,
// or shows failMsg on error. Returns true if success was found.
func (u *UI) waitForResponse(successSubstr, failMsg string) bool {
	deadline := time.Now().Add(3 * time.Second)
	startMsgCount := u.state.MessageCount()
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		u.state.mu.Lock()
		msgs := u.state.messages
		u.state.mu.Unlock()
		for i := startMsgCount; i < len(msgs); i++ {
			if msgs[i].sender == "Error" {
				u.app.QueueUpdateDraw(func() {
					u.showModal(failMsg + ": " + msgs[i].text)
				})
				return false
			}
			if strings.Contains(msgs[i].text, successSubstr) {
				return true
			}
		}
	}
	u.app.QueueUpdateDraw(func() {
		u.showModal(failMsg + ": timed out")
	})
	return false
}

// onMsg runs on the read goroutine: update state, then redraw on the UI thread.
func (u *UI) onMsg(raw string) {
	debugf("recv %q", raw)
	if u.state.Update(raw) {
		u.app.QueueUpdateDraw(u.render)
	}
}

func (u *UI) onClose() {
	u.connected.Store(false)
	u.app.QueueUpdateDraw(func() {
		u.state.mu.Lock()
		u.state.addMessage("System", "Connection lost! Press Q to quit.")
		u.state.mu.Unlock()
		u.render()
	})
}

// onKey dispatches key presses by game phase, mirroring the C handle_*_input.
func (u *UI) onKey(ev *tcell.EventKey) *tcell.EventKey {
	// ESC always quits from any state.
	if ev.Key() == tcell.KeyEscape {
		u.app.Stop()
		return nil
	}

	u.state.mu.Lock()
	inGame, inRoom := u.state.inGame, u.state.inRoom
	u.state.mu.Unlock()

	r := ev.Rune()
	debugf("onKey rune=%q key=%v inGame=%v inRoom=%v", r, ev.Key(), inGame, inRoom)
	if r == 'q' || r == 'Q' {
		if !inGame {
			u.app.Stop()
			return nil
		}
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
	case 'q', 'Q':
		u.send(protocol.BuildSimple("LEAVE_ROOM"))
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
		u.prompt("Room name", func(name string) {
			if strings.TrimSpace(name) == "" {
				name = "Room"
			}
			u.send(protocol.BuildCreateRoom(name, 6))
		})
	case 'l', 'L':
		u.state.mu.Lock()
		u.state.addMessage("System", "Requesting room list...")
		u.state.mu.Unlock()
		u.send(protocol.BuildSimple("LIST_ROOMS"))
		u.render()
	case 'j', 'J':
		u.prompt("Room ID", func(text string) {
			if id := atoi(text); id > 0 {
				u.send(protocol.BuildJoinRoom(id))
			}
		})
	}
}

func (u *UI) send(frame string) {
	connected := u.conn != nil && u.connected.Load()
	debugf("send frame=%q connected=%v", frame, connected)
	if connected {
		if err := u.conn.Send(frame); err != nil {
			debugf("send error: %v", err)
		}
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
	// Adapt the layout so the Messages panel is always visible. In the lobby
	// and room the Arena panel only shows hint text, so shrink it and give the
	// space to Messages; in game it needs its full 22 rows for the map.
	if snap.inGame {
		u.game.ResizeItem(u.world, config.MapHeight+2, 0)
	} else {
		u.game.ResizeItem(u.world, 8, 0)
	}
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
		u.world.SetText("\n   Waiting for game to start...\n\n   Lobby: [C]reate room  [J]oin  [L]ist rooms\n   Room:  [R]eady  [T]chat  [L]eave\n   [Q]/[ESC] Quit")
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
		u.help.SetText("[white]WASD/Arrows[-] Move  [white]J/Space[-] Attack  [white]1-5[-] Item  [white]T[-] Chat  [white]Q[-] Leave")
	case s.inRoom:
		u.help.SetText("[white]R[-] Ready  [white]T[-] Chat  [white]L[-] Leave  [white]Q/ESC[-] Quit")
	default:
		u.help.SetText("[white]C[-] Create  [white]J[-] Join  [white]L[-] List rooms  [white]Q/ESC[-] Quit")
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
