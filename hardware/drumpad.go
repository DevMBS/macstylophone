//go:build darwin

package hardware

import "time"

type DrumpadEvent struct {
	Type      string  `json:"type"`
	Pad       string  `json:"pad"`
	Key       string  `json:"key"`
	Velocity  float64 `json:"velocity"`
	Timestamp float64 `json:"timestamp"`
}

type DrumpadMapper struct{}

func NewDrumpadMapper() *DrumpadMapper {
	return &DrumpadMapper{}
}

func (m *DrumpadMapper) MapInputKey(key InputKey) (DrumpadEvent, bool) {
	now := float64(time.Now().UnixNano()) / 1e9

	switch key {
	case InputKeyDrumKick:
		return DrumpadEvent{Type: "DrumTrigger", Pad: "kick", Key: "z", Velocity: 1.0, Timestamp: now}, true
	case InputKeyDrumSnare:
		return DrumpadEvent{Type: "DrumTrigger", Pad: "snare", Key: "x", Velocity: 1.0, Timestamp: now}, true
	case InputKeyDrumHiHat:
		return DrumpadEvent{Type: "DrumTrigger", Pad: "hihat", Key: "c", Velocity: 1.0, Timestamp: now}, true
	case InputKeyDrumClap:
		return DrumpadEvent{Type: "DrumTrigger", Pad: "clap", Key: "v", Velocity: 1.0, Timestamp: now}, true
	default:
		return DrumpadEvent{}, false
	}
}
