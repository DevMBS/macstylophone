package auth

import (
	"sync"
	"time"
)

type challenge struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

type challengeStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]challenge
}

func newChallengeStore(ttl time.Duration) *challengeStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	return &challengeStore{
		ttl:   ttl,
		items: make(map[string]challenge),
	}
}

func (s *challengeStore) Create(userID string) (*LoginChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	s.cleanupLocked(now)

	id, err := randomToken(24)
	if err != nil {
		return nil, err
	}

	expiresAt := now.Add(s.ttl)
	s.items[id] = challenge{
		ID:        id,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}

	return &LoginChallenge{
		ID:        id,
		ExpiresIn: time.Until(expiresAt).Round(time.Second),
	}, nil
}

func (s *challengeStore) Consume(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	s.cleanupLocked(now)

	item, ok := s.items[id]
	if !ok {
		return "", newError("login_challenge_invalid", "Логин-челлендж недействителен или истёк", "challenge_id", nil)
	}
	delete(s.items, id)

	if now.After(item.ExpiresAt) {
		return "", newError("login_challenge_invalid", "Логин-челлендж недействителен или истёк", "challenge_id", nil)
	}

	return item.UserID, nil
}

func (s *challengeStore) cleanupLocked(now time.Time) {
	for id, item := range s.items {
		if now.After(item.ExpiresAt) {
			delete(s.items, id)
		}
	}
}
