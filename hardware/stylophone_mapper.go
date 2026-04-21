//go:build darwin

package hardware

import (
	"context"
	"fmt"
	"math"
	"sync"
)

var chromaticScale = [...]string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

const notesPerOctave = len(chromaticScale)

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

type TouchpadMapper struct {
	mu            sync.RWMutex
	currentOctave int
	minOctave     int
	maxOctave     int
}

func NewTouchpadMapper(initialOctave, minOctave, maxOctave int) *TouchpadMapper {
	if minOctave > maxOctave {
		minOctave, maxOctave = maxOctave, minOctave
	}

	mapper := &TouchpadMapper{
		minOctave: minOctave,
		maxOctave: maxOctave,
	}
	mapper.currentOctave = mapper.clampOctave(initialOctave)

	return mapper
}

func (m *TouchpadMapper) CurrentOctave() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.currentOctave
}

func (m *TouchpadMapper) ShiftOctave(delta int) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentOctave = m.clampOctave(m.currentOctave + delta)
	return m.currentOctave
}

func (m *TouchpadMapper) SetOctave(octave int) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentOctave = m.clampOctave(octave)
	return m.currentOctave
}

func (m *TouchpadMapper) MapEvent(event MusicEvent) StylophoneEvent {
	octave := m.CurrentOctave()
	noteIndex := noteIndexFromX(event.X)
	effectLevel := effectLevelFromY(event.Y)

	return StylophoneEvent{
		Type:        event.Type.String(),
		FingerID:    event.FingerID,
		Note:        fmt.Sprintf("%s%d", chromaticScale[noteIndex], octave),
		NoteIndex:   noteIndex,
		Octave:      octave,
		EffectLevel: effectLevel,
		X:           event.X,
		Y:           event.Y,
		Pressure:    event.Pressure,
		Timestamp:   event.Timestamp,
	}
}

func (p *MultitouchPoller) StartStylophonePolling(ctx context.Context, mapper *TouchpadMapper, eventChan chan<- StylophoneEvent) error {
	if mapper == nil {
		mapper = NewTouchpadMapper(4, 0, 8)
	}

	rawEventChan := make(chan MusicEvent, 256)
	if err := p.StartEventPolling(ctx, rawEventChan); err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case rawEvent, ok := <-rawEventChan:
				if !ok {
					return
				}
				mapped := mapper.MapEvent(rawEvent)
				select {
				case eventChan <- mapped:
				default:
				}
			}
		}
	}()

	return nil
}

func (m *TouchpadMapper) clampOctave(octave int) int {
	if octave < m.minOctave {
		return m.minOctave
	}
	if octave > m.maxOctave {
		return m.maxOctave
	}
	return octave
}

func noteIndexFromX(x float64) int {
	clampedX := clamp01(x)
	idx := int(clampedX * float64(notesPerOctave))
	if idx >= notesPerOctave {
		idx = notesPerOctave - 1
	}
	return idx
}

func effectLevelFromY(y float64) int {
	clampedY := clamp01(y)
	level := int(math.Round(clampedY * 100.0))
	if level > 100 {
		return 100
	}
	if level < 0 {
		return 0
	}
	return level
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
