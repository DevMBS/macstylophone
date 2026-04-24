package auth

import (
	"context"
	"encoding/json"
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

func (s *Service) GetSynthConfigs(ctx context.Context, userID string) (*UserJSONItems, error) {
	items, err := s.store.GetSynthConfigs(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	return &UserJSONItems{Items: items}, nil
}

func (s *Service) AddSynthConfig(ctx context.Context, userID string, item json.RawMessage) (*UserJSONItems, error) {
	if err := validateJSONObject(item, "config"); err != nil {
		return nil, err
	}

	items, err := s.store.AddSynthConfig(ctx, strings.TrimSpace(userID), item)
	if err != nil {
		return nil, err
	}
	return &UserJSONItems{Items: items}, nil
}

func (s *Service) DeleteSynthConfig(ctx context.Context, userID string, itemID string) (*UserJSONItems, error) {
	normalizedID, err := normalizeUserJSONItemID(itemID)
	if err != nil {
		return nil, err
	}

	items, err := s.store.DeleteSynthConfig(ctx, strings.TrimSpace(userID), normalizedID)
	if err != nil {
		return nil, err
	}
	return &UserJSONItems{Items: items}, nil
}

func (s *Service) GetMelodies(ctx context.Context, userID string) (*UserJSONItems, error) {
	items, err := s.store.GetMelodies(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	return &UserJSONItems{Items: items}, nil
}

func (s *Service) AddMelody(ctx context.Context, userID string, item json.RawMessage) (*UserJSONItems, error) {
	if err := validateJSONObject(item, "melody"); err != nil {
		return nil, err
	}
	if err := validateMelodySnapshotReferences(item); err != nil {
		return nil, err
	}

	items, err := s.store.AddMelody(ctx, strings.TrimSpace(userID), item)
	if err != nil {
		return nil, err
	}
	return &UserJSONItems{Items: items}, nil
}

func (s *Service) DeleteMelody(ctx context.Context, userID string, itemID string) (*UserJSONItems, error) {
	normalizedID, err := normalizeUserJSONItemID(itemID)
	if err != nil {
		return nil, err
	}

	items, err := s.store.DeleteMelody(ctx, strings.TrimSpace(userID), normalizedID)
	if err != nil {
		return nil, err
	}
	return &UserJSONItems{Items: items}, nil
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

func normalizeUserJSONItemID(itemID string) (string, error) {
	normalizedID := strings.TrimSpace(itemID)
	if normalizedID == "" {
		return "", newError("validation_error", "id обязателен", "id", nil)
	}
	return normalizedID, nil
}

func validateJSONObject(item json.RawMessage, field string) error {
	if len(item) == 0 {
		return newError("validation_error", field+" обязателен", field, nil)
	}
	if !json.Valid(item) {
		return newError("validation_error", field+" должен быть валидным JSON", field, nil)
	}

	var object map[string]any
	if err := json.Unmarshal(item, &object); err != nil {
		return newError("validation_error", field+" должен быть JSON-объектом", field, err)
	}
	if object == nil {
		return newError("validation_error", field+" должен быть JSON-объектом", field, nil)
	}

	return nil
}

func validateMelodySnapshotReferences(item json.RawMessage) error {
	var melody map[string]json.RawMessage
	if err := json.Unmarshal(item, &melody); err != nil {
		return newError("validation_error", "melody должен быть JSON-объектом", "melody", err)
	}

	snapshotIDs, err := collectSnapshotIDs(melody["sound_snapshots"])
	if err != nil {
		return err
	}

	if err := validateNotesSnapshotReferences(melody["notes"], snapshotIDs, "notes"); err != nil {
		return err
	}
	if err := validateTracksSnapshotReferences(melody["tracks"], snapshotIDs); err != nil {
		return err
	}

	return nil
}

func collectSnapshotIDs(rawSnapshots json.RawMessage) (map[string]struct{}, error) {
	snapshotIDs := make(map[string]struct{})
	if len(rawSnapshots) == 0 || string(rawSnapshots) == "null" {
		return snapshotIDs, nil
	}

	var snapshots []map[string]json.RawMessage
	if err := json.Unmarshal(rawSnapshots, &snapshots); err != nil {
		return nil, newError("validation_error", "sound_snapshots должен быть массивом JSON-объектов", "sound_snapshots", err)
	}

	for index, snapshot := range snapshots {
		rawID, ok := snapshot["id"]
		if !ok {
			return nil, newError("validation_error", fmt.Sprintf("sound_snapshots[%d].id обязателен", index), "sound_snapshots", nil)
		}

		var id string
		if err := json.Unmarshal(rawID, &id); err != nil || strings.TrimSpace(id) == "" {
			return nil, newError("validation_error", fmt.Sprintf("sound_snapshots[%d].id должен быть непустой строкой", index), "sound_snapshots", err)
		}

		snapshotIDs[id] = struct{}{}
	}

	return snapshotIDs, nil
}

func validateTracksSnapshotReferences(rawTracks json.RawMessage, snapshotIDs map[string]struct{}) error {
	if len(rawTracks) == 0 || string(rawTracks) == "null" {
		return nil
	}

	var tracks []map[string]json.RawMessage
	if err := json.Unmarshal(rawTracks, &tracks); err != nil {
		return newError("validation_error", "tracks должен быть массивом JSON-объектов", "tracks", err)
	}

	for index, track := range tracks {
		if err := validateNotesSnapshotReferences(track["notes"], snapshotIDs, fmt.Sprintf("tracks[%d].notes", index)); err != nil {
			return err
		}
	}

	return nil
}

func validateNotesSnapshotReferences(rawNotes json.RawMessage, snapshotIDs map[string]struct{}, field string) error {
	if len(rawNotes) == 0 || string(rawNotes) == "null" {
		return nil
	}

	var notes []map[string]json.RawMessage
	if err := json.Unmarshal(rawNotes, &notes); err != nil {
		return newError("validation_error", field+" должен быть массивом JSON-объектов", field, err)
	}

	for index, note := range notes {
		rawSnapshotID, ok := note["sound_snapshot_id"]
		if !ok || string(rawSnapshotID) == "null" {
			continue
		}

		var snapshotID string
		if err := json.Unmarshal(rawSnapshotID, &snapshotID); err != nil || strings.TrimSpace(snapshotID) == "" {
			return newError("validation_error", fmt.Sprintf("%s[%d].sound_snapshot_id должен быть непустой строкой", field, index), field, err)
		}
		if _, ok := snapshotIDs[snapshotID]; !ok {
			return newError("validation_error", fmt.Sprintf("%s[%d].sound_snapshot_id ссылается на несуществующий sound_snapshots.id", field, index), field, nil)
		}
	}

	return nil
}
