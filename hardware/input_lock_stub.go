//go:build !darwin

package hardware

import "errors"

type InputKey int

const (
	InputKeyUnknown InputKey = iota
	InputKeyOctaveUp
	InputKeyOctaveDown
	InputKeyEscape
	InputKeyDrumKick
	InputKeyDrumSnare
	InputKeyDrumHiHat
	InputKeyDrumClap
)

type InputLock struct{}

func NewInputLock() *InputLock {
	return &InputLock{}
}

func (l *InputLock) Start() error {
	return errors.New("input lock поддерживается только на darwin")
}

func (l *InputLock) Stop() {}

func (l *InputLock) IsRunning() bool {
	return false
}

func (l *InputLock) DrainKeys() []InputKey {
	return nil
}
