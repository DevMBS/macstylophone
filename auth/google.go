package auth

import (
	"context"
	"fmt"

	"google.golang.org/api/idtoken"
)

type GoogleVerifier interface {
	Verify(ctx context.Context, googleIDToken string) (*GoogleAccount, error)
}

type googleVerifier struct {
	clientID string
}

func newGoogleVerifier(clientID string) (GoogleVerifier, error) {
	if clientID == "" {
		return nil, newError("config_error", "GOOGLE_OAUTH_CLIENT_ID обязателен", "google_client_id", nil)
	}

	return &googleVerifier{clientID: clientID}, nil
}

func (g *googleVerifier) Verify(ctx context.Context, googleIDToken string) (*GoogleAccount, error) {
	payload, err := idtoken.Validate(ctx, googleIDToken, g.clientID)
	if err != nil {
		return nil, newError("invalid_google_token", "Google ID token не прошёл проверку", "google_id_token", err)
	}

	account := &GoogleAccount{
		Subject: payload.Subject,
	}

	if email, ok := payload.Claims["email"].(string); ok {
		account.Email = email
	}
	if emailVerified, ok := payload.Claims["email_verified"].(bool); ok {
		account.EmailVerified = emailVerified
	}
	if name, ok := payload.Claims["name"].(string); ok {
		account.Name = name
	}
	if picture, ok := payload.Claims["picture"].(string); ok {
		account.PictureURL = picture
	}

	if account.Subject == "" {
		return nil, newError("invalid_google_token", "Google token не содержит sub", "google_id_token", nil)
	}
	if account.Email == "" {
		return nil, newError("invalid_google_token", "Google token не содержит email", "google_id_token", nil)
	}
	if !account.EmailVerified {
		return nil, newError("invalid_google_token", "Google email должен быть подтверждён", "google_id_token", nil)
	}

	return account, nil
}

func verifyGoogleMatchesUser(user User, googleAccount *GoogleAccount) error {
	if googleAccount == nil {
		return newError("invalid_google_token", "Google account обязателен", "google_id_token", nil)
	}
	if user.GoogleSubject != googleAccount.Subject {
		return newError("google_account_mismatch", "Google account не совпадает с привязанным аккаунтом", "google_id_token", nil)
	}
	return nil
}

func googleVerifierError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("verify google account: %w", err)
}
