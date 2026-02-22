package database

import (
	"database/sql"
	"fmt"
	"time"
)

func (d *DB) CreateUser(username, passwordHash string, isAdmin bool) (*User, error) {
	result, err := d.db.Exec(
		`INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)`,
		username, passwordHash, isAdmin,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	id, _ := result.LastInsertId()
	return &User{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
		IsAdmin:      isAdmin,
		IsActive:     true,
		CreatedAt:    time.Now(),
	}, nil
}

func (d *DB) GetUserByUsername(username string) (*User, error) {
	u := &User{}
	var isAdmin, isActive int
	var createdAt string
	var lastLogin sql.NullString

	err := d.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, is_active, created_at, last_login_at FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &isActive, &createdAt, &lastLogin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying user: %w", err)
	}

	u.IsAdmin = isAdmin == 1
	u.IsActive = isActive == 1
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	if lastLogin.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", lastLogin.String)
		u.LastLoginAt = &t
	}

	return u, nil
}

func (d *DB) GetUserByID(id int64) (*User, error) {
	u := &User{}
	var isAdmin, isActive int
	var createdAt string
	var lastLogin sql.NullString

	err := d.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, is_active, created_at, last_login_at FROM users WHERE id = ?`,
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &isAdmin, &isActive, &createdAt, &lastLogin)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying user: %w", err)
	}

	u.IsAdmin = isAdmin == 1
	u.IsActive = isActive == 1
	u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	if lastLogin.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", lastLogin.String)
		u.LastLoginAt = &t
	}

	return u, nil
}

func (d *DB) UpdateUserLastLogin(id int64) error {
	_, err := d.db.Exec(
		`UPDATE users SET last_login_at = datetime('now') WHERE id = ?`, id,
	)
	return err
}

func (d *DB) UpdateUserPassword(id int64, passwordHash string) error {
	_, err := d.db.Exec(
		`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id,
	)
	return err
}

func (d *DB) UserCount() (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}
