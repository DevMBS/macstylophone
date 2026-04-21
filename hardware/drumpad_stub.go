//go:build !darwin

package hardware

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
	return DrumpadEvent{}, false
}
