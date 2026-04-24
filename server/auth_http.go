//go:build darwin

package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"stylophone/auth"
)

const maxJSONBodyBytes = 1 << 20

type errorResponse struct {
	Error *wsError `json:"error"`
}

type registerRequest struct {
	Nickname      string `json:"nickname"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	GoogleIDToken string `json:"google_id_token"`
}

type loginStartRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginCompleteRequest struct {
	ChallengeID   string `json:"challenge_id"`
	GoogleIDToken string `json:"google_id_token"`
}

type addSynthConfigRequest struct {
	Config json.RawMessage `json:"config"`
}

type addMelodyRequest struct {
	Melody json.RawMessage `json:"melody"`
}

type loginChallengeResponse struct {
	ChallengeID          string `json:"challenge_id"`
	ExpiresInSeconds     int    `json:"expires_in_seconds"`
	SecondFactorRequired string `json:"second_factor_required"`
}

type jsonItemsResponse struct {
	Items json.RawMessage `json:"items"`
}

func (w *WebSocketMiddleware) handleAuthConfig(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}

	writeJSON(rw, http.StatusOK, map[string]any{
		"google_client_id": w.auth.GoogleClientID(),
		"password_rules":   w.auth.PasswordRules(),
		"nickname_rules":   w.auth.NicknameRules(),
		"second_factor":    "google_oauth",
	})
}

func (w *WebSocketMiddleware) handleNicknameAvailability(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}

	ctx, cancel := requestContext(req.Context(), 10*time.Second)
	defer cancel()

	result, err := w.auth.CheckNickname(ctx, req.URL.Query().Get("nickname"))
	if err != nil {
		writeAuthHTTPError(rw, err)
		return
	}

	writeJSON(rw, http.StatusOK, result)
}

func (w *WebSocketMiddleware) handleRegister(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}

	var payload registerRequest
	if !decodeJSONBody(rw, req, &payload) {
		return
	}

	ctx, cancel := requestContext(req.Context(), 20*time.Second)
	defer cancel()

	session, err := w.auth.Register(ctx, auth.RegisterRequest{
		Nickname:      payload.Nickname,
		Email:         payload.Email,
		Password:      payload.Password,
		GoogleIDToken: payload.GoogleIDToken,
	})
	if err != nil {
		writeAuthHTTPError(rw, err)
		return
	}

	writeJSON(rw, http.StatusCreated, session)
}

func (w *WebSocketMiddleware) handleLoginStart(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}

	var payload loginStartRequest
	if !decodeJSONBody(rw, req, &payload) {
		return
	}

	ctx, cancel := requestContext(req.Context(), 10*time.Second)
	defer cancel()

	challenge, err := w.auth.StartLogin(ctx, auth.LoginStartRequest{
		Email:    payload.Email,
		Password: payload.Password,
	})
	if err != nil {
		writeAuthHTTPError(rw, err)
		return
	}

	writeJSON(rw, http.StatusOK, loginChallengeResponse{
		ChallengeID:          challenge.ID,
		ExpiresInSeconds:     int(challenge.ExpiresIn.Seconds()),
		SecondFactorRequired: "google_oauth",
	})
}

func (w *WebSocketMiddleware) handleLoginComplete(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeMethodNotAllowed(rw, http.MethodPost)
		return
	}

	var payload loginCompleteRequest
	if !decodeJSONBody(rw, req, &payload) {
		return
	}

	ctx, cancel := requestContext(req.Context(), 20*time.Second)
	defer cancel()

	session, err := w.auth.CompleteLogin(ctx, auth.LoginCompleteRequest{
		ChallengeID:   payload.ChallengeID,
		GoogleIDToken: payload.GoogleIDToken,
	})
	if err != nil {
		writeAuthHTTPError(rw, err)
		return
	}

	writeJSON(rw, http.StatusOK, session)
}

func (w *WebSocketMiddleware) handleCurrentSession(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeMethodNotAllowed(rw, http.MethodGet)
		return
	}

	accessToken := extractAccessToken(req)
	if accessToken == "" {
		writeJSON(rw, http.StatusUnauthorized, errorResponse{
			Error: &wsError{
				Code:    "unauthorized",
				Message: "Access token обязателен",
				Field:   "access_token",
			},
		})
		return
	}

	ctx, cancel := requestContext(req.Context(), 10*time.Second)
	defer cancel()

	session, err := w.auth.RestoreSession(ctx, accessToken)
	if err != nil {
		writeAuthHTTPError(rw, err)
		return
	}

	writeJSON(rw, http.StatusOK, session)
}

func (w *WebSocketMiddleware) handleSynthConfigs(rw http.ResponseWriter, req *http.Request) {
	session, ok := w.requireSession(rw, req, 10*time.Second)
	if !ok {
		return
	}
	ctx, cancel := requestContext(req.Context(), 10*time.Second)
	defer cancel()

	switch req.Method {
	case http.MethodGet:
		items, err := w.auth.GetSynthConfigs(ctx, session.User.ID)
		if err != nil {
			writeAuthHTTPError(rw, err)
			return
		}
		writeJSON(rw, http.StatusOK, jsonItemsResponse{Items: items.Items})

	case http.MethodPost:
		var payload addSynthConfigRequest
		if !decodeJSONBody(rw, req, &payload) {
			return
		}
		items, err := w.auth.AddSynthConfig(ctx, session.User.ID, payload.Config)
		if err != nil {
			writeAuthHTTPError(rw, err)
			return
		}
		writeJSON(rw, http.StatusCreated, jsonItemsResponse{Items: items.Items})

	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost)
	}
}

func (w *WebSocketMiddleware) handleSynthConfigByID(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodDelete {
		writeMethodNotAllowed(rw, http.MethodDelete)
		return
	}

	itemID, ok := pathValue(rw, req.URL.Path, "/api/synth/configs/")
	if !ok {
		return
	}

	session, ok := w.requireSession(rw, req, 10*time.Second)
	if !ok {
		return
	}
	ctx, cancel := requestContext(req.Context(), 10*time.Second)
	defer cancel()

	items, err := w.auth.DeleteSynthConfig(ctx, session.User.ID, itemID)
	if err != nil {
		writeAuthHTTPError(rw, err)
		return
	}

	writeJSON(rw, http.StatusOK, jsonItemsResponse{Items: items.Items})
}

func (w *WebSocketMiddleware) handleMelodies(rw http.ResponseWriter, req *http.Request) {
	session, ok := w.requireSession(rw, req, 10*time.Second)
	if !ok {
		return
	}
	ctx, cancel := requestContext(req.Context(), 10*time.Second)
	defer cancel()

	switch req.Method {
	case http.MethodGet:
		items, err := w.auth.GetMelodies(ctx, session.User.ID)
		if err != nil {
			writeAuthHTTPError(rw, err)
			return
		}
		writeJSON(rw, http.StatusOK, jsonItemsResponse{Items: items.Items})

	case http.MethodPost:
		var payload addMelodyRequest
		if !decodeJSONBody(rw, req, &payload) {
			return
		}
		items, err := w.auth.AddMelody(ctx, session.User.ID, payload.Melody)
		if err != nil {
			writeAuthHTTPError(rw, err)
			return
		}
		writeJSON(rw, http.StatusCreated, jsonItemsResponse{Items: items.Items})

	default:
		writeMethodNotAllowed(rw, http.MethodGet+", "+http.MethodPost)
	}
}

func (w *WebSocketMiddleware) handleMelodyByID(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodDelete {
		writeMethodNotAllowed(rw, http.MethodDelete)
		return
	}

	itemID, ok := pathValue(rw, req.URL.Path, "/api/synth/melodies/")
	if !ok {
		return
	}

	session, ok := w.requireSession(rw, req, 10*time.Second)
	if !ok {
		return
	}
	ctx, cancel := requestContext(req.Context(), 10*time.Second)
	defer cancel()

	items, err := w.auth.DeleteMelody(ctx, session.User.ID, itemID)
	if err != nil {
		writeAuthHTTPError(rw, err)
		return
	}

	writeJSON(rw, http.StatusOK, jsonItemsResponse{Items: items.Items})
}

func (w *WebSocketMiddleware) requireSession(rw http.ResponseWriter, req *http.Request, timeout time.Duration) (*auth.Session, bool) {
	accessToken := extractAccessToken(req)
	if accessToken == "" {
		writeJSON(rw, http.StatusUnauthorized, errorResponse{
			Error: &wsError{
				Code:    "unauthorized",
				Message: "Access token обязателен",
				Field:   "access_token",
			},
		})
		return nil, false
	}

	ctx, cancel := requestContext(req.Context(), timeout)
	defer cancel()

	session, err := w.auth.RestoreSession(ctx, accessToken)
	if err != nil {
		writeAuthHTTPError(rw, err)
		return nil, false
	}

	return session, true
}

func pathValue(rw http.ResponseWriter, path string, prefix string) (string, bool) {
	rawValue := strings.TrimPrefix(path, prefix)
	if rawValue == "" || rawValue == path || strings.Contains(rawValue, "/") {
		writeJSON(rw, http.StatusNotFound, errorResponse{
			Error: &wsError{
				Code:    "not_found",
				Message: "Endpoint не найден",
			},
		})
		return "", false
	}

	value, err := url.PathUnescape(rawValue)
	if err != nil || strings.TrimSpace(value) == "" {
		writeJSON(rw, http.StatusBadRequest, errorResponse{
			Error: &wsError{
				Code:    "validation_error",
				Message: "id в URL некорректен",
				Field:   "id",
			},
		})
		return "", false
	}

	return value, true
}

func requestContext(parentCtx context.Context, timeout time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	return ctx, cancel
}

func decodeJSONBody(rw http.ResponseWriter, req *http.Request, target any) bool {
	req.Body = http.MaxBytesReader(rw, req.Body, maxJSONBodyBytes)
	defer func() {
		_ = req.Body.Close()
	}()

	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(rw, http.StatusBadRequest, errorResponse{
			Error: &wsError{
				Code:    "invalid_payload",
				Message: "Невозможно разобрать JSON payload",
			},
		})
		return false
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		writeJSON(rw, http.StatusBadRequest, errorResponse{
			Error: &wsError{
				Code:    "invalid_payload",
				Message: "JSON payload должен содержать только один объект",
			},
		})
		return false
	}

	return true
}

func writeMethodNotAllowed(rw http.ResponseWriter, allowedMethod string) {
	rw.Header().Set("Allow", allowedMethod)
	writeJSON(rw, http.StatusMethodNotAllowed, errorResponse{
		Error: &wsError{
			Code:    "method_not_allowed",
			Message: "HTTP method не поддерживается для этого endpoint",
		},
	})
}

func writeAuthHTTPError(rw http.ResponseWriter, err error) {
	appErr := authError(err)
	if appErr == nil {
		writeJSON(rw, http.StatusInternalServerError, errorResponse{
			Error: &wsError{
				Code:    "internal_error",
				Message: "Внутренняя ошибка сервера",
			},
		})
		return
	}

	writeJSON(rw, statusCodeForAuthError(appErr), errorResponse{
		Error: &wsError{
			Code:    appErr.Code,
			Message: appErr.Message,
			Field:   appErr.Field,
		},
	})
}

func statusCodeForAuthError(err *auth.Error) int {
	switch err.Code {
	case "validation_error", "invalid_google_token", "google_account_mismatch", "login_challenge_invalid":
		return http.StatusBadRequest
	case "invalid_credentials", "unauthorized":
		return http.StatusUnauthorized
	case "nickname_taken", "email_taken", "google_account_taken":
		return http.StatusConflict
	case "not_found":
		return http.StatusNotFound
	case "config_error":
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(rw http.ResponseWriter, statusCode int, payload any) {
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(statusCode)
	_ = json.NewEncoder(rw).Encode(payload)
}
