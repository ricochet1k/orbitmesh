package terminal

import (
	"errors"

	"github.com/ricochet1k/termemu"
)

// SendInput dispatches a terminal input to the given termemu terminal.
func SendInput(term termemu.Terminal, input Input) error {
	if term == nil {
		return errors.New("terminal not initialized")
	}

	switch input.Kind {
	case InputKey:
		if input.Key == nil {
			return errors.New("missing key input")
		}
		ev := termemu.KeyEvent{
			Code:       input.Key.Code,
			Rune:       input.Key.Rune,
			Mod:        input.Key.Mod,
			Event:      input.Key.Event,
			Shifted:    input.Key.Shifted,
			BaseLayout: input.Key.BaseLayout,
			Text:       input.Key.Text,
		}
		_, err := term.SendKey(ev)
		return err

	case InputText:
		if input.Text == nil {
			return errors.New("missing text input")
		}
		_, err := term.Write([]byte(input.Text.Text))
		return err

	case InputMouse:
		if input.Mouse == nil {
			return errors.New("missing mouse input")
		}
		if mouseTerm, ok := term.(interface {
			SendMouseRaw(btn termemu.MouseBtn, press bool, mods termemu.MouseFlag, x, y int) error
		}); ok {
			return mouseTerm.SendMouseRaw(input.Mouse.Button, input.Mouse.Press, input.Mouse.Mods, input.Mouse.X, input.Mouse.Y)
		}
		return errors.New("mouse input not supported")

	case InputResize:
		if input.Resize == nil {
			return errors.New("missing resize input")
		}
		return term.Resize(input.Resize.Cols, input.Resize.Rows)

	case InputControl:
		if input.Control == nil {
			return errors.New("missing control input")
		}
		var payload []byte
		switch input.Control.Signal {
		case ControlInterrupt:
			payload = []byte{0x03}
		case ControlEOF:
			payload = []byte{0x04}
		case ControlSuspend:
			payload = []byte{0x1a}
		default:
			return errors.New("unknown control signal")
		}
		_, err := term.Write(payload)
		return err

	case InputRaw:
		if input.Raw == nil {
			return errors.New("missing raw input")
		}
		_, err := term.Write(input.Raw.Data)
		return err

	default:
		return errors.New("unsupported input")
	}
}
