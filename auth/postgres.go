package auth

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type postgresStore struct {
	pool *pgxpool.Pool
}

func newPostgresStore(ctx context.Context, databaseURL string) (*postgresStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, newError("config_error", "DATABASE_URL обязателен", "database_url", nil)
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &postgresStore{pool: pool}
	if err := store.applyMigrations(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return store, nil
}

func (s *postgresStore) Close() {
	if s == nil || s.pool == nil {
		return
	}
	s.pool.Close()
}

func (s *postgresStore) applyMigrations(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists {
			continue
		}

		migration, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(migration)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("register migration %s: %w", name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}

	return nil
}

func (s *postgresStore) IsNicknameAvailable(ctx context.Context, normalizedNickname string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM auth_users
			WHERE nickname_normalized = $1
		)
	`, normalizedNickname).Scan(&exists); err != nil {
		return false, fmt.Errorf("check nickname availability: %w", err)
	}
	return !exists, nil
}

func (s *postgresStore) CreateUser(ctx context.Context, user User) (*User, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO auth_users (
			id,
			nickname,
			nickname_normalized,
			email,
			email_normalized,
			password_hash,
			google_subject,
			google_email,
			google_email_verified,
			google_name,
			google_picture_url
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING
			id,
			nickname,
			nickname_normalized,
			email,
			email_normalized,
			password_hash,
			google_subject,
			google_email,
			google_email_verified,
			google_name,
			google_picture_url,
			created_at,
			updated_at
	`,
		user.ID,
		user.Nickname,
		user.NicknameNormalized,
		user.Email,
		user.EmailNormalized,
		user.PasswordHash,
		user.GoogleSubject,
		user.GoogleEmail,
		user.GoogleEmailVerified,
		nullIfEmpty(user.GoogleName),
		nullIfEmpty(user.GooglePictureURL),
	)

	createdUser, err := scanUser(row)
	if err == nil {
		return createdUser, nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "auth_users_nickname_normalized_key":
			return nil, newError("nickname_taken", "Никнейм уже занят", "nickname", err)
		case "auth_users_email_normalized_key":
			return nil, newError("email_taken", "Email уже зарегистрирован", "email", err)
		case "auth_users_google_subject_key":
			return nil, newError("google_account_taken", "Google account уже привязан к другому пользователю", "google_id_token", err)
		}
	}

	return nil, fmt.Errorf("create user: %w", err)
}

func (s *postgresStore) GetUserByEmail(ctx context.Context, normalizedEmail string) (*User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			id,
			nickname,
			nickname_normalized,
			email,
			email_normalized,
			password_hash,
			google_subject,
			google_email,
			google_email_verified,
			google_name,
			google_picture_url,
			created_at,
			updated_at
		FROM auth_users
		WHERE email_normalized = $1
	`, normalizedEmail)
	return scanUser(row)
}

func (s *postgresStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			id,
			nickname,
			nickname_normalized,
			email,
			email_normalized,
			password_hash,
			google_subject,
			google_email,
			google_email_verified,
			google_name,
			google_picture_url,
			created_at,
			updated_at
		FROM auth_users
		WHERE id = $1
	`, id)
	return scanUser(row)
}

func scanUser(row pgx.Row) (*User, error) {
	var user User
	if err := row.Scan(
		&user.ID,
		&user.Nickname,
		&user.NicknameNormalized,
		&user.Email,
		&user.EmailNormalized,
		&user.PasswordHash,
		&user.GoogleSubject,
		&user.GoogleEmail,
		&user.GoogleEmailVerified,
		&user.GoogleName,
		&user.GooglePictureURL,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, newError("not_found", "Пользователь не найден", "", err)
		}
		return nil, err
	}
	return &user, nil
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
