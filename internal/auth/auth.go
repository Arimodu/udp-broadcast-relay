package auth

import (
	"fmt"
	"time"

	"github.com/Arimodu/udp-broadcast-relay/internal/database"

	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db *database.DB
}

func NewService(db *database.DB) *Service {
	return &Service{db: db}
}

// AuthenticatePassword verifies username/password and returns user + new session token.
func (s *Service) AuthenticatePassword(username, password string) (*database.User, string, error) {
	user, err := s.db.GetUserByUsername(username)
	if err != nil {
		return nil, "", fmt.Errorf("looking up user: %w", err)
	}
	if user == nil {
		return nil, "", nil
	}
	if !user.IsActive {
		return nil, "", nil
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", nil
	}

	s.db.UpdateUserLastLogin(user.ID)

	// Create session
	token := GenerateSessionToken()
	expiresAt := time.Now().Add(24 * time.Hour)
	if _, err := s.db.CreateSession(user.ID, token, expiresAt); err != nil {
		return nil, "", fmt.Errorf("creating session: %w", err)
	}

	return user, token, nil
}

// ValidateSession checks if a session token is valid and returns the user.
func (s *Service) ValidateSession(token string) (*database.User, error) {
	session, err := s.db.GetSessionByToken(token)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, nil
	}

	user, err := s.db.GetUserByID(session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, nil
	}

	return user, nil
}

// ValidateAPIKey checks if an API key is valid and returns the key + user.
func (s *Service) ValidateAPIKey(key string) (*database.APIKey, *database.User, error) {
	return s.db.ValidateAPIKey(key)
}

// Logout invalidates a session token.
func (s *Service) Logout(token string) error {
	return s.db.DeleteSessionByToken(token)
}

// HashPassword creates a bcrypt hash.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
