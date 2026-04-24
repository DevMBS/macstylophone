package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type Service struct {
	store      *postgresStore
	google     GoogleVerifier
	tokens     *tokenManager
	challenges *challengeStore
}

func NewService(ctx context.Context, cfg Config) (*Service, error) {
	store, err := newPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	googleVerifier, err := newGoogleVerifier(cfg.GoogleClientID)
	if err != nil {
		store.Close()
		return nil, err
	}

	tokens, err := newTokenManager(cfg)
	if err != nil {
		store.Close()
		return nil, err
	}

	return &Service{
		store:      store,
		google:     googleVerifier,
		tokens:     tokens,
		challenges: newChallengeStore(cfg.LoginChallengeTTL),
	}, nil
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.store.Close()
}

func (s *Service) NicknameRules() map[string]any {
	return map[string]any{
		"min_length": minNicknameLength,
		"max_length": maxNicknameLength,
		"pattern":    nicknamePattern.String(),
	}
}

func (s *Service) PasswordRules() map[string]any {
	return map[string]any{
		"min_length": minPasswordLength,
		"max_length": maxPasswordLength,
	}
}

func (s *Service) GoogleClientID() string {
	if verifier, ok := s.google.(*googleVerifier); ok {
		return verifier.clientID
	}
	return ""
}

func (s *Service) CheckNickname(ctx context.Context, nickname string) (*NicknameAvailability, error) {
	normalizedNickname, normalized, err := normalizeNickname(nickname)
	if err != nil {
		return nil, err
	}

	available, err := s.store.IsNicknameAvailable(ctx, normalized)
	if err != nil {
		return nil, err
	}

	return &NicknameAvailability{
		Nickname:   normalizedNickname,
		Normalized: normalized,
		Available:  available,
	}, nil
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*Session, error) {
	nickname, normalizedNickname, err := normalizeNickname(req.Nickname)
	if err != nil {
		return nil, err
	}

	email, err := normalizeEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(req.Password, email, nickname); err != nil {
		return nil, err
	}

	googleAccount, err := s.google.Verify(ctx, strings.TrimSpace(req.GoogleIDToken))
	if err != nil {
		return nil, googleVerifierError(err)
	}

	passwordHash, err := hashPassword(req.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := User{
		ID:                  newUUID(),
		Nickname:            nickname,
		NicknameNormalized:  normalizedNickname,
		Email:               email,
		EmailNormalized:     email,
		PasswordHash:        passwordHash,
		GoogleSubject:       googleAccount.Subject,
		GoogleEmail:         strings.ToLower(googleAccount.Email),
		GoogleEmailVerified: googleAccount.EmailVerified,
		GoogleName:          googleAccount.Name,
		GooglePictureURL:    googleAccount.PictureURL,
	}

	createdUser, err := s.store.CreateUser(ctx, user)
	if err != nil {
		return nil, err
	}

	return s.issueSession(*createdUser)
}

func (s *Service) StartLogin(ctx context.Context, req LoginStartRequest) (*LoginChallenge, error) {
	email, err := normalizeEmail(req.Email)
	if err != nil {
		return nil, err
	}

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if appErr := asAppError(err); appErr != nil && appErr.Code == "not_found" {
			return nil, newError("invalid_credentials", "Неверный email или пароль", "", nil)
		}
		return nil, err
	}

	passwordValid, err := verifyPassword(user.PasswordHash, req.Password)
	if err != nil {
		return nil, fmt.Errorf("verify password: %w", err)
	}
	if !passwordValid {
		return nil, newError("invalid_credentials", "Неверный email или пароль", "", nil)
	}

	return s.challenges.Create(user.ID)
}

func (s *Service) CompleteLogin(ctx context.Context, req LoginCompleteRequest) (*Session, error) {
	challengeID := strings.TrimSpace(req.ChallengeID)
	if challengeID == "" {
		return nil, newError("validation_error", "challenge_id обязателен", "challenge_id", nil)
	}

	userID, err := s.challenges.Consume(challengeID)
	if err != nil {
		return nil, err
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	googleAccount, err := s.google.Verify(ctx, strings.TrimSpace(req.GoogleIDToken))
	if err != nil {
		return nil, googleVerifierError(err)
	}
	if err := verifyGoogleMatchesUser(*user, googleAccount); err != nil {
		return nil, err
	}

	return s.issueSession(*user)
}

func (s *Service) RestoreSession(ctx context.Context, accessToken string) (*Session, error) {
	claims, err := s.tokens.Parse(strings.TrimSpace(accessToken))
	if err != nil {
		return nil, err
	}

	user, err := s.store.GetUserByID(ctx, claims.UserID)
	if err != nil {
		if appErr := asAppError(err); appErr != nil && appErr.Code == "not_found" {
			return nil, newError("unauthorized", "Пользователь из access token не найден", "", err)
		}
		return nil, err
	}

	return &Session{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresAt:   claims.ExpiresAt.Time,
		User:        *user,
	}, nil
}

func (s *Service) issueSession(user User) (*Session, error) {
	token, expiresAt, err := s.tokens.Issue(user)
	if err != nil {
		return nil, err
	}

	return &Session{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		User:        user,
	}, nil
}

func asAppError(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return nil
}
