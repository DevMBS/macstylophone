package auth

import (
	"context"
	"embed"
	"encoding/json"
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
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	//noinspection SqlNoDataSourceInspection
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
		//noinspection SqlNoDataSourceInspection
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
		//noinspection SqlNoDataSourceInspection
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
	//noinspection SqlNoDataSourceInspection
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
	//noinspection SqlNoDataSourceInspection
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
	//noinspection SqlNoDataSourceInspection
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
	//noinspection SqlNoDataSourceInspection
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

func (s *postgresStore) GetSynthConfigs(ctx context.Context, userID string) (json.RawMessage, error) {
	return s.getJSONItems(ctx, userID, "synth_configs")
}

func (s *postgresStore) AddSynthConfig(ctx context.Context, userID string, item json.RawMessage) (json.RawMessage, error) {
	return s.addJSONItem(ctx, userID, "synth_configs", item)
}

func (s *postgresStore) DeleteSynthConfig(ctx context.Context, userID string, itemID string) (json.RawMessage, error) {
	return s.deleteJSONItem(ctx, userID, "synth_configs", itemID)
}

func (s *postgresStore) GetMelodies(ctx context.Context, userID string) (json.RawMessage, error) {
	return s.getJSONItems(ctx, userID, "melodies")
}

func (s *postgresStore) AddMelody(ctx context.Context, userID string, item json.RawMessage) (json.RawMessage, error) {
	return s.addJSONItem(ctx, userID, "melodies", item)
}

func (s *postgresStore) DeleteMelody(ctx context.Context, userID string, itemID string) (json.RawMessage, error) {
	return s.deleteJSONItem(ctx, userID, "melodies", itemID)
}

func (s *postgresStore) getJSONItems(ctx context.Context, userID string, column string) (json.RawMessage, error) {
	query, err := userJSONSelectQuery(column)
	if err != nil {
		return nil, err
	}

	var items json.RawMessage
	//noinspection SqlNoDataSourceInspection
	if err := s.pool.QueryRow(ctx, query, userID).Scan(&items); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, newError("not_found", "Пользователь не найден", "", err)
		}
		return nil, fmt.Errorf("get %s: %w", column, err)
	}

	return items, nil
}

func (s *postgresStore) addJSONItem(ctx context.Context, userID string, column string, item json.RawMessage) (json.RawMessage, error) {
	query, err := userJSONAppendQuery(column)
	if err != nil {
		return nil, err
	}

	var items json.RawMessage
	//noinspection SqlNoDataSourceInspection
	if err := s.pool.QueryRow(ctx, query, userID, string(item)).Scan(&items); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, newError("not_found", "Пользователь не найден", "", err)
		}
		return nil, fmt.Errorf("append %s: %w", column, err)
	}

	return items, nil
}

func (s *postgresStore) deleteJSONItem(ctx context.Context, userID string, column string, itemID string) (json.RawMessage, error) {
	existsQuery, err := userJSONItemExistsQuery(column)
	if err != nil {
		return nil, err
	}

	var exists bool
	//noinspection SqlNoDataSourceInspection
	if err := s.pool.QueryRow(ctx, existsQuery, userID, itemID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, newError("not_found", "Пользователь не найден", "", err)
		}
		return nil, fmt.Errorf("check %s item existence: %w", column, err)
	}
	if !exists {
		return nil, newError("not_found", "Элемент не найден", "id", nil)
	}

	deleteQuery, err := userJSONDeleteQuery(column)
	if err != nil {
		return nil, err
	}

	var items json.RawMessage
	//noinspection SqlNoDataSourceInspection
	if err := s.pool.QueryRow(ctx, deleteQuery, userID, itemID).Scan(&items); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, newError("not_found", "Пользователь не найден", "", err)
		}
		return nil, fmt.Errorf("delete %s item: %w", column, err)
	}

	return items, nil
}

func userJSONSelectQuery(column string) (string, error) {
	if err := validateUserJSONColumn(column); err != nil {
		return "", err
	}
	return fmt.Sprintf(`SELECT %s FROM auth_users WHERE id = $1`, column), nil
}

func userJSONAppendQuery(column string) (string, error) {
	if err := validateUserJSONColumn(column); err != nil {
		return "", err
	}
	return fmt.Sprintf(`
		UPDATE auth_users
		SET %s = %s || jsonb_build_array($2::jsonb),
			updated_at = NOW()
		WHERE id = $1
		RETURNING %s
	`, column, column, column), nil
}

func userJSONItemExistsQuery(column string) (string, error) {
	if err := validateUserJSONColumn(column); err != nil {
		return "", err
	}
	return fmt.Sprintf(`
		SELECT EXISTS(
			SELECT 1
			FROM jsonb_array_elements(%s) AS item
			WHERE item->>'id' = $2
		)
		FROM auth_users
		WHERE id = $1
	`, column), nil
}

func userJSONDeleteQuery(column string) (string, error) {
	if err := validateUserJSONColumn(column); err != nil {
		return "", err
	}
	return fmt.Sprintf(`
		UPDATE auth_users
		SET %s = COALESCE(
				(
					SELECT jsonb_agg(item)
					FROM jsonb_array_elements(%s) AS item
					WHERE item->>'id' <> $2
				),
				'[]'::jsonb
			),
			updated_at = NOW()
		WHERE id = $1
		RETURNING %s
	`, column, column, column), nil
}

func validateUserJSONColumn(column string) error {
	switch column {
	case "synth_configs", "melodies":
		return nil
	default:
		return fmt.Errorf("unsupported user json column: %s", column)
	}
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
