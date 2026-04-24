package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type tokenManager struct {
	issuer string
	secret []byte
	ttl    time.Duration
}

type sessionClaims struct {
	UserID   string   `json:"uid"`
	Email    string   `json:"email"`
	Nickname string   `json:"nickname"`
	AMR      []string `json:"amr"`
	jwt.RegisteredClaims
}

func newTokenManager(cfg Config) (*tokenManager, error) {
	if len(cfg.JWTSecret) < 32 {
		return nil, newError("config_error", "AUTH_JWT_SECRET должен быть не короче 32 символов", "jwt_secret", nil)
	}

	issuer := cfg.JWTIssuer
	if issuer == "" {
		issuer = "stylophone-middleware"
	}

	ttl := cfg.AccessTokenTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	return &tokenManager{
		issuer: issuer,
		secret: []byte(cfg.JWTSecret),
		ttl:    ttl,
	}, nil
}

func (m *tokenManager) Issue(user User) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(m.ttl)
	jti, err := randomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}

	claims := sessionClaims{
		UserID:   user.ID,
		Email:    user.Email,
		Nickname: user.Nickname,
		AMR:      []string{"pwd", "google"},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   user.ID,
			Audience:  []string{"stylophone-websocket"},
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}

	return signed, expiresAt, nil
}

func (m *tokenManager) Parse(tokenString string) (*sessionClaims, error) {
	claims := &sessionClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return m.secret, nil
	}, jwt.WithAudience("stylophone-websocket"), jwt.WithIssuer(m.issuer))
	if err != nil {
		return nil, newError("unauthorized", "Недействительный access token", "access_token", err)
	}
	if !token.Valid {
		return nil, newError("unauthorized", "Недействительный access token", "access_token", nil)
	}

	return claims, nil
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
