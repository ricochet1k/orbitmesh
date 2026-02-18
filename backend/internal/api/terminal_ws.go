package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/ricochet1k/orbitmesh/internal/service"
	"github.com/ricochet1k/orbitmesh/internal/terminal"
	"github.com/ricochet1k/termemu"
)

const terminalProtocolVersion = 1

var terminalWriteDelay time.Duration

type terminalEnvelope struct {
	Version   int       `json:"v"`
	Type      string    `json:"type"`
	SessionID string    `json:"session_id"`
	Seq       int64     `json:"seq"`
	TS        time.Time `json:"ts"`
	Data      any       `json:"data,omitempty"`
}

type terminalInboundMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type terminalInputError struct {
	code    string
	message string
}

func (e *terminalInputError) Error() string {
	if e == nil {
		return "terminal input error"
	}
	return e.message
}

func (h *Handler) terminalWebSocket(w http.ResponseWriter, r *http.Request) {
	if !defaultPermissions.CanInspectSessions {
		writeError(w, http.StatusForbidden, "session inspection not allowed", "")
		return
	}

	sessionID := chi.URLParam(r, "id")
	if _, err := h.executor.GetSession(sessionID); err != nil {
		if errors.Is(err, service.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to look up session", err.Error())
		return
	}

	hub, err := h.executor.TerminalHub(sessionID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTerminalNotSupported):
			writeError(w, http.StatusConflict, "terminal not available", "")
		case errors.Is(err, service.ErrSessionNotFound):
			writeError(w, http.StatusNotFound, "session not found", "")
		default:
			writeError(w, http.StatusInternalServerError, "failed to start terminal", err.Error())
		}
		return
	}

	allowInput := terminalWriteRequested(r)
	allowRaw := allowInput && terminalRawRequested(r)
	if allowInput && !csrfTokenMatches(r) {
		writeError(w, http.StatusForbidden, "invalid CSRF token", "csrf header mismatch")
		return
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	updates, cancel := hub.Subscribe(0)

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for event := range updates {
			if err := writeTerminalEvent(conn, sessionID, event); err != nil {
				return
			}
			if terminalWriteDelay > 0 {
				time.Sleep(terminalWriteDelay)
			}
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if len(data) == 0 {
			continue
		}
		if err := handleTerminalInput(r.Context(), hub, sessionID, allowInput, allowRaw, data, conn); err != nil {
			break
		}
	}

	cancel()
	<-writeDone
}

func handleTerminalInput(ctx context.Context, hub *service.TerminalHub, sessionID string, allowInput, allowRaw bool, data []byte, conn *websocket.Conn) error {
	var msg terminalInboundMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return sendTerminalError(conn, sessionID, hub.NextSeq(), "bad_request", "invalid message")
	}

	if msg.Type == "" {
		return sendTerminalError(conn, sessionID, hub.NextSeq(), "bad_request", "missing message type")
	}
	if !strings.HasPrefix(msg.Type, "input.") {
		return sendTerminalError(conn, sessionID, hub.NextSeq(), "unsupported", "unsupported message type")
	}
	if !allowInput {
		return sendTerminalError(conn, sessionID, hub.NextSeq(), "forbidden", "terminal input not allowed")
	}

	input, err := parseTerminalInput(msg, allowRaw)
	if err != nil {
		if te, ok := err.(*terminalInputError); ok {
			return sendTerminalError(conn, sessionID, hub.NextSeq(), te.code, te.message)
		}
		return sendTerminalError(conn, sessionID, hub.NextSeq(), "bad_request", err.Error())
	}

	if err := hub.HandleInput(ctx, input); err != nil {
		return sendTerminalError(conn, sessionID, hub.NextSeq(), "input_failed", err.Error())
	}
	return nil
}

func parseTerminalInput(msg terminalInboundMessage, allowRaw bool) (terminal.Input, error) {
	switch msg.Type {
	case "input.text":
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			return terminal.Input{}, err
		}
		return terminal.Input{Kind: terminal.InputText, Text: &terminal.TextInput{Text: payload.Text}}, nil
	case "input.key":
		return parseKeyInput(msg.Data)
	case "input.mouse":
		return parseMouseInput(msg.Data)
	case "input.resize":
		var payload struct {
			Cols int `json:"cols"`
			Rows int `json:"rows"`
		}
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			return terminal.Input{}, err
		}
		return terminal.Input{Kind: terminal.InputResize, Resize: &terminal.ResizeInput{Cols: payload.Cols, Rows: payload.Rows}}, nil
	case "input.control":
		var payload struct {
			Signal string `json:"signal"`
		}
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			return terminal.Input{}, err
		}
		signal, err := parseControlSignal(payload.Signal)
		if err != nil {
			return terminal.Input{}, err
		}
		return terminal.Input{Kind: terminal.InputControl, Control: &terminal.ControlInput{Signal: signal}}, nil
	case "input.raw":
		if !allowRaw {
			return terminal.Input{}, &terminalInputError{code: "forbidden", message: "raw input not permitted"}
		}
		var payload struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			return terminal.Input{}, err
		}
		decoded, err := base64.StdEncoding.DecodeString(payload.Data)
		if err != nil {
			return terminal.Input{}, err
		}
		return terminal.Input{Kind: terminal.InputRaw, Raw: &terminal.RawInput{Data: decoded}}, nil
	default:
		return terminal.Input{}, &terminalInputError{code: "unsupported", message: "unsupported input type"}
	}
}

func parseKeyInput(data []byte) (terminal.Input, error) {
	var payload struct {
		Code  any      `json:"code"`
		Rune  string   `json:"rune"`
		Mod   []string `json:"mod"`
		Event string   `json:"event"`
		Shift string   `json:"shifted"`
		Base  string   `json:"base"`
		Text  string   `json:"text"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return terminal.Input{}, err
	}
	code, err := parseKeyCode(payload.Code)
	if err != nil {
		return terminal.Input{}, err
	}
	if code == termemu.KeyRune && payload.Rune == "" {
		return terminal.Input{}, &terminalInputError{code: "bad_request", message: "rune required for KeyRune"}
	}
	key := &terminal.KeyInput{
		Code:       code,
		Rune:       firstRune(payload.Rune),
		Mod:        parseKeyMods(payload.Mod),
		Event:      parseKeyEvent(payload.Event),
		Shifted:    firstRune(payload.Shift),
		BaseLayout: firstRune(payload.Base),
		Text:       []rune(payload.Text),
	}
	return terminal.Input{Kind: terminal.InputKey, Key: key}, nil
}

func parseMouseInput(data []byte) (terminal.Input, error) {
	var payload struct {
		Button any      `json:"button"`
		Action string   `json:"action"`
		X      int      `json:"x"`
		Y      int      `json:"y"`
		Mod    []string `json:"mod"`
		Wheel  string   `json:"wheel"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return terminal.Input{}, err
	}
	button, press, mods, err := parseMouseFields(payload.Button, payload.Action, payload.Wheel, payload.Mod)
	if err != nil {
		return terminal.Input{}, err
	}
	return terminal.Input{Kind: terminal.InputMouse, Mouse: &terminal.MouseInput{Button: button, Press: press, Mods: mods, X: payload.X, Y: payload.Y}}, nil
}

func parseControlSignal(signal string) (terminal.ControlSignal, error) {
	switch strings.ToLower(strings.TrimSpace(signal)) {
	case "interrupt", "sigint":
		return terminal.ControlInterrupt, nil
	case "eof":
		return terminal.ControlEOF, nil
	case "suspend", "sigstp":
		return terminal.ControlSuspend, nil
	default:
		return terminal.ControlInterrupt, &terminalInputError{code: "bad_request", message: "unknown control signal"}
	}
}

func parseKeyCode(code any) (termemu.KeyCode, error) {
	if code == nil {
		return termemu.KeyRune, nil
	}
	switch v := code.(type) {
	case float64:
		return termemu.KeyCode(int(v)), nil
	case int:
		return termemu.KeyCode(v), nil
	case string:
		return keyCodeFromString(v)
	default:
		return termemu.KeyRune, &terminalInputError{code: "bad_request", message: "invalid key code"}
	}
}

func keyCodeFromString(input string) (termemu.KeyCode, error) {
	name := strings.ToLower(strings.TrimSpace(input))
	switch name {
	case "rune":
		return termemu.KeyRune, nil
	case "up":
		return termemu.KeyUp, nil
	case "down":
		return termemu.KeyDown, nil
	case "left":
		return termemu.KeyLeft, nil
	case "right":
		return termemu.KeyRight, nil
	case "home":
		return termemu.KeyHome, nil
	case "end":
		return termemu.KeyEnd, nil
	case "insert":
		return termemu.KeyInsert, nil
	case "delete":
		return termemu.KeyDelete, nil
	case "pageup", "page_up":
		return termemu.KeyPageUp, nil
	case "pagedown", "page_down":
		return termemu.KeyPageDown, nil
	case "backspace":
		return termemu.KeyBackspace, nil
	case "tab":
		return termemu.KeyTab, nil
	case "enter":
		return termemu.KeyEnter, nil
	case "escape", "esc":
		return termemu.KeyEscape, nil
	case "f1":
		return termemu.KeyF1, nil
	case "f2":
		return termemu.KeyF2, nil
	case "f3":
		return termemu.KeyF3, nil
	case "f4":
		return termemu.KeyF4, nil
	case "f5":
		return termemu.KeyF5, nil
	case "f6":
		return termemu.KeyF6, nil
	case "f7":
		return termemu.KeyF7, nil
	case "f8":
		return termemu.KeyF8, nil
	case "f9":
		return termemu.KeyF9, nil
	case "f10":
		return termemu.KeyF10, nil
	case "f11":
		return termemu.KeyF11, nil
	case "f12":
		return termemu.KeyF12, nil
	default:
		return termemu.KeyRune, &terminalInputError{code: "bad_request", message: "unknown key code"}
	}
}

func parseKeyMods(mods []string) termemu.KeyMod {
	var out termemu.KeyMod
	for _, mod := range mods {
		switch strings.ToLower(strings.TrimSpace(mod)) {
		case "shift":
			out |= termemu.ModShift
		case "alt":
			out |= termemu.ModAlt
		case "ctrl", "control":
			out |= termemu.ModCtrl
		case "super":
			out |= termemu.ModSuper
		case "hyper":
			out |= termemu.ModHyper
		case "meta":
			out |= termemu.ModMeta
		case "capslock", "caps_lock":
			out |= termemu.ModCapsLock
		case "numlock", "num_lock":
			out |= termemu.ModNumLock
		}
	}
	return out
}

func parseKeyEvent(value string) termemu.KeyEventType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "repeat":
		return termemu.KeyRepeat
	case "release":
		return termemu.KeyRelease
	case "press":
		return termemu.KeyPress
	default:
		return termemu.KeyPress
	}
}

func parseMouseFields(button any, action, wheel string, mods []string) (termemu.MouseBtn, bool, termemu.MouseFlag, error) {
	modsOut := parseMouseMods(mods)
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "move" {
		modsOut |= termemu.MMotion
	}
	if action == "wheel" {
		modsOut |= termemu.MWheel
	}
	press := action != "release"
	if action == "release" {
		press = false
	}

	btn, err := parseMouseButton(button, wheel)
	if err != nil {
		return termemu.MBtn1, false, modsOut, err
	}
	return btn, press, modsOut, nil
}

func parseMouseButton(button any, wheel string) (termemu.MouseBtn, error) {
	if wheel != "" {
		switch strings.ToLower(strings.TrimSpace(wheel)) {
		case "up":
			return termemu.MBtn1, nil
		case "down":
			return termemu.MBtn2, nil
		}
	}
	switch v := button.(type) {
	case float64:
		switch int(v) {
		case 1:
			return termemu.MBtn1, nil
		case 2:
			return termemu.MBtn2, nil
		case 3:
			return termemu.MBtn3, nil
		case 0:
			return termemu.MRelease, nil
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "left":
			return termemu.MBtn1, nil
		case "middle":
			return termemu.MBtn2, nil
		case "right":
			return termemu.MBtn3, nil
		case "release":
			return termemu.MRelease, nil
		}
	case nil:
		return termemu.MBtn1, nil
	}
	return termemu.MBtn1, &terminalInputError{code: "bad_request", message: "invalid mouse button"}
}

func parseMouseMods(mods []string) termemu.MouseFlag {
	var out termemu.MouseFlag
	for _, mod := range mods {
		switch strings.ToLower(strings.TrimSpace(mod)) {
		case "shift":
			out |= termemu.MShift
		case "alt", "meta":
			out |= termemu.MMeta
		case "ctrl", "control":
			out |= termemu.MControl
		}
	}
	return out
}

func firstRune(value string) rune {
	if value == "" {
		return 0
	}
	return []rune(value)[0]
}

func writeTerminalEvent(conn *websocket.Conn, sessionID string, event service.TerminalEvent) error {
	update := event.Update
	var (
		messageType string
		payload     any
	)

	switch update.Kind {
	case terminal.UpdateSnapshot:
		messageType = "terminal.snapshot"
		if update.Snapshot != nil {
			payload = map[string]any{
				"rows":  update.Snapshot.Rows,
				"cols":  update.Snapshot.Cols,
				"lines": update.Snapshot.Lines,
			}
		}
	case terminal.UpdateDiff:
		messageType = "terminal.diff"
		if update.Diff != nil {
			payload = map[string]any{
				"region": map[string]int{
					"x":  update.Diff.Region.X,
					"y":  update.Diff.Region.Y,
					"x2": update.Diff.Region.X2,
					"y2": update.Diff.Region.Y2,
				},
				"lines":  update.Diff.Lines,
				"reason": update.Diff.Reason,
			}
		}
	case terminal.UpdateCursor:
		messageType = "terminal.cursor"
		if update.Cursor != nil {
			payload = map[string]int{"x": update.Cursor.X, "y": update.Cursor.Y}
		}
	case terminal.UpdateBell:
		messageType = "terminal.bell"
	case terminal.UpdateError:
		messageType = "terminal.error"
		if update.Error != nil {
			payload = map[string]any{
				"code":    update.Error.Code,
				"message": update.Error.Message,
				"resync":  update.Error.Resync,
			}
		}
	default:
		return nil
	}

	envelope := terminalEnvelope{
		Version:   terminalProtocolVersion,
		Type:      messageType,
		SessionID: sessionID,
		Seq:       event.Seq,
		TS:        time.Now().UTC(),
		Data:      payload,
	}
	return conn.WriteJSON(envelope)
}

func sendTerminalError(conn *websocket.Conn, sessionID string, seq int64, code, message string) error {
	envelope := terminalEnvelope{
		Version:   terminalProtocolVersion,
		Type:      "terminal.error",
		SessionID: sessionID,
		Seq:       seq,
		TS:        time.Now().UTC(),
		Data: map[string]any{
			"code":    code,
			"message": message,
		},
	}
	return conn.WriteJSON(envelope)
}

func terminalWriteRequested(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("X-Terminal-Write"), "true") {
		return true
	}
	q := r.URL.Query()
	if strings.EqualFold(q.Get("write"), "true") {
		return true
	}
	return strings.EqualFold(q.Get("mode"), "write")
}

func terminalRawRequested(r *http.Request) bool {
	q := r.URL.Query()
	return strings.EqualFold(q.Get("allow_raw"), "true")
}

func csrfTokenMatches(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	// Accept the token from the X-CSRF-Token header or, for WebSocket
	// connections where custom headers are not supported by browsers, from
	// the csrf_token query parameter.
	candidate := r.Header.Get(csrfHeaderName)
	if candidate == "" {
		candidate = r.URL.Query().Get("csrf_token")
	}
	if candidate == "" {
		return false
	}
	return candidate == cookie.Value
}
