package webpush

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DBStore struct {
	pool *pgxpool.Pool
}

func NewDBStore(pool *pgxpool.Pool) *DBStore {
	return &DBStore{pool: pool}
}

func (s *DBStore) Upsert(ctx context.Context, userID string, subscription Subscription) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO web_push_subscription (user_id, endpoint, p256dh, auth)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (endpoint) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			p256dh = EXCLUDED.p256dh,
			auth = EXCLUDED.auth,
			updated_at = now()
	`, userID, subscription.Endpoint, subscription.P256DH, subscription.Auth)
	return err
}

func (s *DBStore) DeleteForUser(ctx context.Context, userID, endpoint string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM web_push_subscription WHERE user_id = $1::uuid AND endpoint = $2
	`, userID, endpoint)
	return err
}

func (s *DBStore) DeleteByEndpoint(ctx context.Context, endpoint string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM web_push_subscription WHERE endpoint = $1`, endpoint)
	return err
}

func (s *DBStore) ListByUser(ctx context.Context, userID string) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT endpoint, p256dh, auth
		FROM web_push_subscription
		WHERE user_id = $1::uuid
		ORDER BY created_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subscriptions []Subscription
	for rows.Next() {
		var subscription Subscription
		if err := rows.Scan(&subscription.Endpoint, &subscription.P256DH, &subscription.Auth); err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, rows.Err()
}

func (s *DBStore) NotificationPreferences(ctx context.Context, workspaceID, userID string) (map[string]string, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT preferences
		FROM notification_preference
		WHERE workspace_id = $1::uuid AND user_id = $2::uuid
	`, workspaceID, userID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	preferences := map[string]string{}
	if err := json.Unmarshal(raw, &preferences); err != nil {
		return map[string]string{}, nil
	}
	return preferences, nil
}

func (s *DBStore) WorkspaceSlug(ctx context.Context, workspaceID string) (string, error) {
	var slug string
	err := s.pool.QueryRow(ctx, `SELECT slug FROM workspace WHERE id = $1::uuid`, workspaceID).Scan(&slug)
	return slug, err
}
