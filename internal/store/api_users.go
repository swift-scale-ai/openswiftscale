package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type APIUser struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email,omitempty"`
	Enabled   bool      `json:"enabled"`
	Keys      []APIKey  `json:"keys"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type APIKey struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Enabled    bool       `json:"enabled"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (s *Store) APIUsers(ctx context.Context) ([]APIUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,email,enabled,created_at,updated_at FROM api_users ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]APIUser, 0)
	index := make(map[string]int)
	for rows.Next() {
		var user APIUser
		var enabled int
		var created, updated string
		if err := rows.Scan(&user.ID, &user.Name, &user.Email, &enabled, &created, &updated); err != nil {
			return nil, err
		}
		user.Enabled = enabled == 1
		user.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		user.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		user.Keys = []APIKey{}
		index[user.ID] = len(users)
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	keyRows, err := s.db.QueryContext(ctx, `SELECT id,user_id,name,prefix,enabled,last_used_at,created_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer keyRows.Close()
	for keyRows.Next() {
		var key APIKey
		var enabled int
		var lastUsed sql.NullString
		var created string
		if err := keyRows.Scan(&key.ID, &key.UserID, &key.Name, &key.Prefix, &enabled, &lastUsed, &created); err != nil {
			return nil, err
		}
		key.Enabled = enabled == 1
		key.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if lastUsed.Valid {
			value, parseErr := time.Parse(time.RFC3339Nano, lastUsed.String)
			if parseErr == nil {
				key.LastUsedAt = &value
			}
		}
		if position, ok := index[key.UserID]; ok {
			users[position].Keys = append(users[position].Keys, key)
		}
	}
	return users, keyRows.Err()
}

func (s *Store) CreateAPIUser(ctx context.Context, user APIUser) error {
	now := time.Now().UTC()
	user.ID = strings.TrimSpace(user.ID)
	user.Name = strings.TrimSpace(user.Name)
	user.Email = strings.TrimSpace(user.Email)
	if user.ID == "" || user.Name == "" {
		return fmt.Errorf("user ID and name are required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_users(id,name,email,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?)`,
		user.ID, user.Name, user.Email, boolInt(user.Enabled), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) UpdateAPIUser(ctx context.Context, id, name, email string, enabled bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE api_users SET name=?,email=?,enabled=?,updated_at=? WHERE id=?`,
		strings.TrimSpace(name), strings.TrimSpace(email), boolInt(enabled), time.Now().UTC().Format(time.RFC3339Nano), strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return fmt.Errorf("API user was not found")
	}
	return nil
}

func (s *Store) DeleteAPIUser(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM api_users WHERE id=?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return fmt.Errorf("API user was not found")
	}
	return nil
}

func (s *Store) CreateAPIKey(ctx context.Context, key APIKey, plaintext string) error {
	digest := sha256.Sum256([]byte(strings.TrimSpace(plaintext)))
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_keys(id,user_id,name,prefix,key_hash,enabled,created_at) VALUES(?,?,?,?,?,1,?)`,
		key.ID, key.UserID, strings.TrimSpace(key.Name), key.Prefix, digest[:], time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) DeleteAPIKey(ctx context.Context, userID, keyID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id=? AND user_id=?`, strings.TrimSpace(keyID), strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return fmt.Errorf("API key was not found")
	}
	return nil
}

func (s *Store) AuthenticateAPIKey(ctx context.Context, plaintext string) (string, bool, error) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(plaintext)))
	var keyID string
	err := s.db.QueryRowContext(ctx, `SELECT k.id FROM api_keys k JOIN api_users u ON u.id=k.user_id WHERE k.key_hash=? AND k.enabled=1 AND u.enabled=1`, digest[:]).Scan(&keyID)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=? AND (last_used_at IS NULL OR last_used_at<?)`,
		time.Now().UTC().Format(time.RFC3339Nano), keyID, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano))
	return keyID, true, nil
}
