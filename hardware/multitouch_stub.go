//go:build !darwin

package hardware

import (
	"context"
	"errors"
	"time"
)

var ErrUnsupportedPlatform = errors.New("multitouch доступен только на darwin")

type TouchState int

const (
	TouchStateNotTouching TouchState = 0
	TouchStateTouching    TouchState = 1
	TouchStateLifted      TouchState = 7
)

func (s TouchState) IsTouching() bool {
	return false
}

type TouchFrame struct {
	FingerID  int
	X         float64
	Y         float64
	Pressure  float64
	State     TouchState
	Timestamp float64
}

type EventType int

const (
	EventSynthStart EventType = iota
	EventSynthMove
	EventSynthEnd
)

func (e EventType) String() string {
	return "Unsupported"
}

type MusicEvent struct {
	Type      EventType `json:"type"`
	FingerID  int       `json:"finger_id"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	Pressure  float64   `json:"pressure"`
	Timestamp float64   `json:"timestamp"`
}

type MultitouchPoller struct{}

func NewMultitouchPoller(pollInterval time.Duration) *MultitouchPoller {
	return &MultitouchPoller{}
}

func (p *MultitouchPoller) Start() error {
	return ErrUnsupportedPlatform
}

func (p *MultitouchPoller) Stop() {}

func (p *MultitouchPoller) StartEventPolling(ctx context.Context, eventChan chan<- MusicEvent) error {
	return ErrUnsupportedPlatform
}
