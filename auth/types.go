package auth

import "time"

type Config struct {
	DatabaseURL       string
	GoogleClientID    string
	JWTSecret         string
	JWTIssuer         string
	AccessTokenTTL    time.Duration
	LoginChallengeTTL time.Duration
}

type User struct {
	ID                  string    `json:"id"`
	Nickname            string    `json:"nickname"`
	NicknameNormalized  string    `json:"-"`
	Email               string    `json:"email"`
	EmailNormalized     string    `json:"-"`
	PasswordHash        string    `json:"-"`
	GoogleSubject       string    `json:"-"`
	GoogleEmail         string    `json:"google_email"`
	GoogleEmailVerified bool      `json:"google_email_verified"`
	GoogleName          string    `json:"google_name,omitempty"`
	GooglePictureURL    string    `json:"google_picture_url,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type GoogleAccount struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	PictureURL    string
}

type RegisterRequest struct {
	Nickname      string
	Email         string
	Password      string
	GoogleIDToken string
}

type LoginStartRequest struct {
	Email    string
	Password string
}

type LoginChallenge struct {
	ID        string        `json:"challenge_id"`
	ExpiresIn time.Duration `json:"expires_in"`
}

type LoginCompleteRequest struct {
	ChallengeID   string
	GoogleIDToken string
}

type Session struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
	User        User      `json:"user"`
}

type NicknameAvailability struct {
	Nickname   string `json:"nickname"`
	Normalized string `json:"normalized"`
	Available  bool   `json:"available"`
}
