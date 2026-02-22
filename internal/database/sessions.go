package database

import (
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) CreateSession(userID int64, token string, expiresAt time.Time) (*Session, error) {
	result, err := d.db.Exec(
		`INSERT INTO sessions (user_id, token, expires_at) VALUES (?, ?, ?)`,
		userID, token, expiresAt.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return nil, fmt.Errorf("inserting session: %w", err)
	}

	id, _ := result.LastInsertId()
	return &Session{
		ID:        id,
		UserID:    userID,
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}, nil
}

func (d *DB) GetSessionByToken(token string) (*Session, error) {
	s := &Session{}
	var createdAt, expiresAt string

	err := d.db.QueryRow(
		`SELECT id, user_id, token, created_at, expires_at FROM sessions WHERE token = ?`,
		token,
	).Scan(&s.ID, &s.UserID, &s.Token, &createdAt, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying session: %w", err)
	}

	s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	s.ExpiresAt, _ = time.Parse("2006-01-02 15:04:05", expiresAt)

	if s.ExpiresAt.Before(time.Now()) {
		d.DeleteSession(s.ID)
		return nil, nil
	}

	return s, nil
}

func (d *DB) DeleteSession(id int64) error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (d *DB) DeleteSessionByToken(token string) error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (d *DB) CleanExpiredSessions() error {
	_, err := d.db.Exec(`DELETE FROM sessions WHERE expires_at < datetime('now')`)
	return err
}
