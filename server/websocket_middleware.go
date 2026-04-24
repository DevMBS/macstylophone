//go:build darwin

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"stylophone/auth"
	"stylophone/hardware"
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
	DatabaseURL      string
	GoogleClientID   string
	JWTSecret        string
	JWTIssuer        string
	AccessTokenTTL   time.Duration
	ChallengeTTL     time.Duration
}

type envelope struct {
	RequestID string      `json:"id,omitempty"`
	Type      string      `json:"type"`
	Data      interface{} `json:"data,omitempty"`
	Error     *wsError    `json:"error,omitempty"`
}

type wsError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type inboundEnvelope struct {
	RequestID string          `json:"id,omitempty"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
}

type shiftOctavePayload struct {
	Delta int `json:"delta"`
}

type setOctavePayload struct {
	Value int `json:"value"`
}

type WebSocketMiddleware struct {
	config     Config
	httpServer *http.Server
	poller     *hardware.MultitouchPoller
	mapper     *hardware.TouchpadMapper
	drums      *hardware.DrumpadMapper
	inputLock  *hardware.InputLock
	gestures   *hardware.GestureSuppressor
	hub        *clientHub
	auth       *auth.Service
}

func NewWebSocketMiddleware(cfg Config) (*WebSocketMiddleware, error) {
	config := withDefaults(cfg)

	initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	authService, err := auth.NewService(initCtx, auth.Config{
		DatabaseURL:       config.DatabaseURL,
		GoogleClientID:    config.GoogleClientID,
		JWTSecret:         config.JWTSecret,
		JWTIssuer:         config.JWTIssuer,
		AccessTokenTTL:    config.AccessTokenTTL,
		LoginChallengeTTL: config.ChallengeTTL,
	})
	if err != nil {
		return nil, err
	}

	w := &WebSocketMiddleware{
		config:    config,
		poller:    hardware.NewMultitouchPoller(config.PollInterval),
		mapper:    hardware.NewTouchpadMapper(config.InitialOctave, config.MinOctave, config.MaxOctave),
		drums:     hardware.NewDrumpadMapper(),
		inputLock: hardware.NewInputLock(),
		gestures:  hardware.NewDockOnlyGestureSuppressor(),
		hub:       newClientHub(),
		auth:      authService,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", w.handleWebSocket)
	mux.HandleFunc("/api/auth/config", w.handleAuthConfig)
	mux.HandleFunc("/api/auth/nickname-availability", w.handleNicknameAvailability)
	mux.HandleFunc("/api/auth/register", w.handleRegister)
	mux.HandleFunc("/api/auth/login/start", w.handleLoginStart)
	mux.HandleFunc("/api/auth/login/complete", w.handleLoginComplete)
	mux.HandleFunc("/api/auth/me", w.handleCurrentSession)
	mux.HandleFunc("/api/synth/configs", w.handleSynthConfigs)
	mux.HandleFunc("/api/synth/configs/", w.handleSynthConfigByID)
	mux.HandleFunc("/api/synth/melodies", w.handleMelodies)
	mux.HandleFunc("/api/synth/melodies/", w.handleMelodyByID)
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("ok"))
	})

	w.httpServer = &http.Server{
		Addr:              config.Address,
		Handler:           corsMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return w, nil
}

func (w *WebSocketMiddleware) Run(ctx context.Context) error {
	httpErrChan := make(chan error, 1)
	stylophoneChan := make(chan hardware.StylophoneEvent, 512)

	defer w.auth.Close()

	inputLockEnabled := false
	if !w.config.DisableInputLock {
		if !w.config.DisableGestures {
			if err := w.gestures.Start(); err != nil {
				return fmt.Errorf("не удалось отключить жесты трекпада: %w", err)
			}
			defer w.gestures.Stop()
			log.Println("[input] Системные жесты трекпада временно отключены.")
		}

		if err := w.inputLock.Start(); err != nil {
			return err
		}
		inputLockEnabled = true
		defer w.inputLock.Stop()
		log.Println("[input] Курсор и жесты заблокированы. Esc завершит работу.")
	}

	if err := w.poller.StartStylophonePolling(ctx, w.mapper, stylophoneChan); err != nil {
		return fmt.Errorf("не удалось запустить polling тачпада: %w", err)
	}

	go func() {
		err := w.httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrChan <- err
		}
	}()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = w.httpServer.Shutdown(shutdownCtx)
		w.hub.closeAll()
	}()

	keyTicker := time.NewTicker(10 * time.Millisecond)
	defer keyTicker.Stop()
	pingTicker := time.NewTicker(25 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.hub.broadcast(envelope{Type: "stylophone:status", Data: map[string]any{"status": "stopped"}})
			return nil
		case err := <-httpErrChan:
			return fmt.Errorf("http сервер завершился: %w", err)
		case mapped := <-stylophoneChan:
			w.hub.broadcast(envelope{Type: "stylophone:event", Data: mapped})
		case <-pingTicker.C:
			w.hub.pingAll()
		case <-keyTicker.C:
			if !inputLockEnabled {
				continue
			}
			if err := w.handleInputKeys(); err != nil {
				w.hub.broadcast(envelope{Type: "stylophone:status", Data: map[string]any{
					"status": "stopped",
					"reason": "escape",
				}})
				return err
			}
		}
	}
}

func (w *WebSocketMiddleware) handleInputKeys() error {
	for _, key := range w.inputLock.DrainKeys() {
		switch key {
		case hardware.InputKeyOctaveUp:
			octave := w.mapper.ShiftOctave(1)
			w.hub.broadcast(envelope{Type: "stylophone:octave", Data: map[string]int{"value": octave}})
		case hardware.InputKeyOctaveDown:
			octave := w.mapper.ShiftOctave(-1)
			w.hub.broadcast(envelope{Type: "stylophone:octave", Data: map[string]int{"value": octave}})
		case hardware.InputKeyEscape:
			return ErrEscapePressed
		default:
			if drum, ok := w.drums.MapInputKey(key); ok {
				w.hub.broadcast(envelope{Type: "drumpad:event", Data: drum})
			}
		}
	}

	return nil
}

func (w *WebSocketMiddleware) handleWebSocket(rw http.ResponseWriter, req *http.Request) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(_ *http.Request) bool {
			return true
		},
	}

	conn, err := upgrader.Upgrade(rw, req, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	client := newWSClient(conn)
	log.Printf("[ws] client connected: %s", conn.RemoteAddr())

	if accessToken := extractAccessToken(req); accessToken != "" {
		if session, err := w.auth.RestoreSession(context.Background(), accessToken); err == nil {
			w.authenticateClient(client, session)
			w.sendStatusSnapshot(client)
		} else {
			w.writeAuthError(client, "", err)
		}
	}
	if !client.isAuthenticated() {
		_ = client.writeJSON(envelope{Type: "auth:required", Data: map[string]any{
			"first_factor":  "email_password",
			"second_factor": "google_oauth",
		}})
	}

	go func() {
		defer func() {
			w.hub.remove(client)
			_ = conn.Close()
			log.Printf("[ws] client disconnected: %s", conn.RemoteAddr())
		}()

		for {
			var msg inboundEnvelope
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			w.handleClientMessage(client, msg)
		}
	}()
}

func (w *WebSocketMiddleware) handleClientMessage(client *wsClient, msg inboundEnvelope) {
	switch msg.Type {
	case "ping":
		_ = client.writeJSON(envelope{RequestID: msg.RequestID, Type: "pong", Data: map[string]any{"ts": time.Now().UnixMilli()}})
		return
	}

	if !client.isAuthenticated() {
		_ = client.writeJSON(envelope{
			RequestID: msg.RequestID,
			Type:      "error",
			Error: &wsError{
				Code:    "unauthorized",
				Message: "Сначала завершите аутентификацию по email/паролю и Google OAuth",
			},
		})
		return
	}

	switch msg.Type {
	case "stylophone:shift-octave":
		var payload shiftOctavePayload
		if !w.decodePayload(client, msg, &payload) {
			return
		}
		delta := payload.Delta
		if delta > 1 {
			delta = 1
		}
		if delta < -1 {
			delta = -1
		}
		octave := w.mapper.ShiftOctave(delta)
		w.hub.broadcast(envelope{Type: "stylophone:octave", Data: map[string]int{"value": octave}})

	case "stylophone:set-octave":
		var payload setOctavePayload
		if !w.decodePayload(client, msg, &payload) {
			return
		}
		octave := w.mapper.SetOctave(payload.Value)
		w.hub.broadcast(envelope{Type: "stylophone:octave", Data: map[string]int{"value": octave}})

	default:
		_ = client.writeJSON(envelope{
			RequestID: msg.RequestID,
			Type:      "error",
			Error: &wsError{
				Code:    "unknown_message_type",
				Message: "Неизвестный тип websocket-сообщения",
			},
		})
	}
}

func (w *WebSocketMiddleware) authenticateClient(client *wsClient, session *auth.Session) {
	if client.markAuthenticated(session.User.ID) {
		w.hub.add(client)
	}
}

func (w *WebSocketMiddleware) sendStatusSnapshot(client *wsClient) {
	_ = client.writeJSON(envelope{Type: "stylophone:status", Data: map[string]any{
		"status": "ready",
		"octave": w.mapper.CurrentOctave(),
	}})
}

func (w *WebSocketMiddleware) decodePayload(client *wsClient, msg inboundEnvelope, target any) bool {
	if len(msg.Data) == 0 {
		if err := json.Unmarshal([]byte("{}"), target); err != nil {
			w.writeJSONError(client, msg.RequestID, "invalid_payload", "Невозможно разобрать payload", "")
			return false
		}
		return true
	}
	if err := json.Unmarshal(msg.Data, target); err != nil {
		w.writeJSONError(client, msg.RequestID, "invalid_payload", "Невозможно разобрать payload", "")
		return false
	}
	return true
}

func (w *WebSocketMiddleware) writeAuthError(client *wsClient, requestID string, err error) {
	if appErr := authError(err); appErr != nil {
		w.writeJSONError(client, requestID, appErr.Code, appErr.Message, appErr.Field)
		return
	}
	log.Printf("[auth] unexpected error: %v", err)
	w.writeJSONError(client, requestID, "internal_error", "Внутренняя ошибка сервера", "")
}

func (w *WebSocketMiddleware) writeJSONError(client *wsClient, requestID, code, message, field string) {
	_ = client.writeJSON(envelope{
		RequestID: requestID,
		Type:      "error",
		Error: &wsError{
			Code:    code,
			Message: message,
			Field:   field,
		},
	})
}

func authError(err error) *auth.Error {
	var appErr *auth.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return nil
}

func extractAccessToken(req *http.Request) string {
	accessToken := strings.TrimSpace(req.URL.Query().Get("access_token"))
	if accessToken != "" {
		return accessToken
	}

	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}

	return ""
}

func withDefaults(cfg Config) Config {
	if cfg.Address == "" {
		cfg.Address = ":8090"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 15 * time.Millisecond
	}
	if cfg.MinOctave > cfg.MaxOctave {
		cfg.MinOctave, cfg.MaxOctave = cfg.MaxOctave, cfg.MinOctave
	}
	if cfg.MinOctave == 0 && cfg.MaxOctave == 0 {
		cfg.MinOctave = 0
		cfg.MaxOctave = 8
	}
	if cfg.InitialOctave < cfg.MinOctave {
		cfg.InitialOctave = cfg.MinOctave
	}
	if cfg.InitialOctave > cfg.MaxOctave {
		cfg.InitialOctave = cfg.MaxOctave
	}
	return cfg
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type clientHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

func newClientHub() *clientHub {
	return &clientHub{
		clients: make(map[*wsClient]struct{}),
	}
}

func (h *clientHub) add(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *clientHub) remove(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
}

func (h *clientHub) broadcast(msg envelope) {
	h.mu.RLock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		if err := c.writeJSON(msg); err != nil {
			h.remove(c)
			_ = c.close()
			log.Printf("[ws] client write failed and removed: %v", err)
		}
	}
}

func (h *clientHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		_ = c.close()
		delete(h.clients, c)
	}
}

func (h *clientHub) pingAll() {
	h.mu.RLock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		if err := c.writeControlPing(); err != nil {
			h.remove(c)
			_ = c.close()
		}
	}
}

type wsClient struct {
	conn          *websocket.Conn
	mu            sync.Mutex
	authMu        sync.RWMutex
	authenticated bool
	userID        string
}

func newWSClient(conn *websocket.Conn) *wsClient {
	conn.SetPongHandler(func(_ string) error {
		return conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	})
	_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	return &wsClient{conn: conn}
}

func (c *wsClient) markAuthenticated(userID string) bool {
	c.authMu.Lock()
	defer c.authMu.Unlock()

	wasAuthenticated := c.authenticated
	c.authenticated = true
	c.userID = userID
	return !wasAuthenticated
}

func (c *wsClient) isAuthenticated() bool {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.authenticated
}

func (c *wsClient) writeJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return c.conn.WriteJSON(v)
}

func (c *wsClient) writeControlPing() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	return c.conn.WriteControl(websocket.PingMessage, nil, deadline)
}

func (c *wsClient) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}
