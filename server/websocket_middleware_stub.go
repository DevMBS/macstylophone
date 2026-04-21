//go:build !darwin

package server

import (
	"context"
	"errors"
	"time"
)

var ErrEscapePressed = errors.New("escape pressed")

type Config struct {
	Address          string
	PollInterval     time.Duration
	InitialOctave    int
	MinOctave        int
	MaxOctave        int
	DisableInputLock bool
	DisableGestures  bool
}

type WebSocketMiddleware struct{}

func NewWebSocketMiddleware(cfg Config) (*WebSocketMiddleware, error) {
	return nil, errors.New("websocket middleware поддерживается только на darwin")
}

func (s *WebSocketMiddleware) Run(ctx context.Context) error {
	return errors.New("websocket middleware поддерживается только на darwin")
}
