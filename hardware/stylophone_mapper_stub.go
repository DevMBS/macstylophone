//go:build !darwin

package hardware

import "context"

type StylophoneEvent struct {
	Type        string  `json:"type"`
	FingerID    int     `json:"finger_id"`
	Note        string  `json:"note"`
	NoteIndex   int     `json:"note_index"`
	Octave      int     `json:"octave"`
	EffectLevel int     `json:"effect_level"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Pressure    float64 `json:"pressure"`
	Timestamp   float64 `json:"timestamp"`
}

type TouchpadMapper struct{}

func NewTouchpadMapper(initialOctave, minOctave, maxOctave int) *TouchpadMapper {
	return &TouchpadMapper{}
}

func (m *TouchpadMapper) CurrentOctave() int {
	return 0
}

func (m *TouchpadMapper) ShiftOctave(delta int) int {
	return 0
}

func (m *TouchpadMapper) SetOctave(octave int) int {
	return 0
}

func (m *TouchpadMapper) MapEvent(event MusicEvent) StylophoneEvent {
	return StylophoneEvent{}
}

func (p *MultitouchPoller) StartStylophonePolling(ctx context.Context, mapper *TouchpadMapper, eventChan chan<- StylophoneEvent) error {
	return ErrUnsupportedPlatform
}
