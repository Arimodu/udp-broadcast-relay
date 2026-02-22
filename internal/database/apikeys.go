package database

import (
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) CreateAPIKey(userID int64, key, name string) (*APIKey, error) {
	result, err := d.db.Exec(
		`INSERT INTO api_keys (user_id, key, name) VALUES (?, ?, ?)`,
		userID, key, name,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting api key: %w", err)
	}

	id, _ := result.LastInsertId()
	return &APIKey{
		ID:        id,
		UserID:    userID,
		Key:       key,
		Name:      name,
		CreatedAt: time.Now(),
	}, nil
}

func (d *DB) GetAPIKeyByKey(key string) (*APIKey, error) {
	ak := &APIKey{}
	var isRevoked int
	var createdAt string
	var lastUsed, expiresAt sql.NullString

	err := d.db.QueryRow(
		`SELECT id, user_id, key, name, created_at, last_used_at, expires_at, is_revoked FROM api_keys WHERE key = ?`,
		key,
	).Scan(&ak.ID, &ak.UserID, &ak.Key, &ak.Name, &createdAt, &lastUsed, &expiresAt, &isRevoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying api key: %w", err)
	}

	ak.IsRevoked = isRevoked == 1
	ak.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	if lastUsed.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", lastUsed.String)
		ak.LastUsedAt = &t
	}
	if expiresAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", expiresAt.String)
		ak.ExpiresAt = &t
	}

	return ak, nil
}

func (d *DB) ListAPIKeysByUser(userID int64) ([]APIKey, error) {
	rows, err := d.db.Query(
		`SELECT id, user_id, key, name, created_at, last_used_at, expires_at, is_revoked FROM api_keys WHERE user_id = ? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying api keys: %w", err)
	}
	defer rows.Close()

	return scanAPIKeys(rows)
}

func (d *DB) ListAllAPIKeys() ([]APIKey, error) {
	rows, err := d.db.Query(
		`SELECT id, user_id, key, name, created_at, last_used_at, expires_at, is_revoked FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying api keys: %w", err)
	}
	defer rows.Close()

	return scanAPIKeys(rows)
}

func scanAPIKeys(rows *sql.Rows) ([]APIKey, error) {
	var keys []APIKey
	for rows.Next() {
		var ak APIKey
		var isRevoked int
		var createdAt string
		var lastUsed, expiresAt sql.NullString

		if err := rows.Scan(&ak.ID, &ak.UserID, &ak.Key, &ak.Name, &createdAt, &lastUsed, &expiresAt, &isRevoked); err != nil {
			return nil, fmt.Errorf("scanning api key: %w", err)
		}

		ak.IsRevoked = isRevoked == 1
		ak.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		if lastUsed.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", lastUsed.String)
			ak.LastUsedAt = &t
		}
		if expiresAt.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", expiresAt.String)
			ak.ExpiresAt = &t
		}
		keys = append(keys, ak)
	}
	return keys, rows.Err()
}

func (d *DB) UpdateAPIKeyLastUsed(id int64) error {
	_, err := d.db.Exec(
		`UPDATE api_keys SET last_used_at = datetime('now') WHERE id = ?`, id,
	)
	return err
}

func (d *DB) RevokeAPIKey(id int64) error {
	_, err := d.db.Exec(
		`UPDATE api_keys SET is_revoked = 1 WHERE id = ?`, id,
	)
	return err
}

func (d *DB) DeleteAPIKey(id int64) error {
	_, err := d.db.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

func (d *DB) ValidateAPIKey(key string) (*APIKey, *User, error) {
	ak, err := d.GetAPIKeyByKey(key)
	if err != nil {
		return nil, nil, err
	}
	if ak == nil {
		return nil, nil, nil
	}
	if ak.IsRevoked {
		return nil, nil, nil
	}
	if ak.ExpiresAt != nil && ak.ExpiresAt.Before(time.Now()) {
		return nil, nil, nil
	}

	user, err := d.GetUserByID(ak.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil || !user.IsActive {
		return nil, nil, nil
	}

	d.UpdateAPIKeyLastUsed(ak.ID)
	d.UpdateUserLastLogin(user.ID)

	return ak, user, nil
}
