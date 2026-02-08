package terminal

import "github.com/ricochet1k/termemu"

type Region struct {
	X  int
	Y  int
	X2 int
	Y2 int
}

type Snapshot struct {
	Rows  int
	Cols  int
	Lines []string
}

type Diff struct {
	Region Region
	Lines  []string
	Reason string
}

type Cursor struct {
	X int
	Y int
}

type Error struct {
	Code    string
	Message string
	Resync  bool
}

type UpdateKind int

const (
	UpdateSnapshot UpdateKind = iota
	UpdateDiff
	UpdateCursor
	UpdateBell
	UpdateError
)

type Update struct {
	Kind     UpdateKind
	Snapshot *Snapshot
	Diff     *Diff
	Cursor   *Cursor
	Error    *Error
}

type InputKind int

const (
	InputKey InputKind = iota
	InputText
	InputMouse
	InputResize
	InputControl
	InputRaw
)

type KeyInput struct {
	Code       termemu.KeyCode
	Rune       rune
	Mod        termemu.KeyMod
	Event      termemu.KeyEventType
	Shifted    rune
	BaseLayout rune
	Text       []rune
}

type TextInput struct {
	Text string
}

type MouseInput struct {
	Button termemu.MouseBtn
	Press  bool
	Mods   termemu.MouseFlag
	X      int
	Y      int
}

type ResizeInput struct {
	Cols int
	Rows int
}

type ControlSignal int

const (
	ControlInterrupt ControlSignal = iota
	ControlEOF
	ControlSuspend
)

type ControlInput struct {
	Signal ControlSignal
}

type RawInput struct {
	Data []byte
}

type Input struct {
	Kind    InputKind
	Key     *KeyInput
	Text    *TextInput
	Mouse   *MouseInput
	Resize  *ResizeInput
	Control *ControlInput
	Raw     *RawInput
}
