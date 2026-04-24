package auth

import (
	"net/mail"
	"regexp"
	"strings"
)

const (
	minNicknameLength = 3
	maxNicknameLength = 32
	minPasswordLength = 12
	maxPasswordLength = 128
)

var nicknamePattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{1,30}[A-Za-z0-9])?$`)

func normalizeEmail(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", newError("validation_error", "Email обязателен", "email", nil)
	}

	parsed, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", newError("validation_error", "Некорректный email", "email", err)
	}

	addr := strings.ToLower(strings.TrimSpace(parsed.Address))
	if addr == "" {
		return "", newError("validation_error", "Некорректный email", "email", nil)
	}

	return addr, nil
}

func normalizeNickname(nickname string) (string, string, error) {
	trimmed := strings.TrimSpace(nickname)
	if trimmed == "" {
		return "", "", newError("validation_error", "Никнейм обязателен", "nickname", nil)
	}
	if len(trimmed) < minNicknameLength || len(trimmed) > maxNicknameLength {
		return "", "", newError("validation_error", "Никнейм должен быть длиной от 3 до 32 символов", "nickname", nil)
	}
	if !nicknamePattern.MatchString(trimmed) {
		return "", "", newError("validation_error", "Никнейм может содержать только буквы, цифры, '.', '_' и '-'", "nickname", nil)
	}

	return trimmed, strings.ToLower(trimmed), nil
}

func validatePassword(password, email, nickname string) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return newError("validation_error", "Пароль должен быть длиной от 12 до 128 символов", "password", nil)
	}
	if strings.TrimSpace(password) == "" {
		return newError("validation_error", "Пароль не может состоять только из пробелов", "password", nil)
	}

	lowerPassword := strings.ToLower(password)
	if email != "" && strings.Contains(lowerPassword, strings.ToLower(email)) {
		return newError("validation_error", "Пароль не должен содержать email", "password", nil)
	}
	if nickname != "" && strings.Contains(lowerPassword, strings.ToLower(nickname)) {
		return newError("validation_error", "Пароль не должен содержать никнейм", "password", nil)
	}

	return nil
}
