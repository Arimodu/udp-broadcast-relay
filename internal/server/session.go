package server

import (
	"sync"

	"github.com/Arimodu/udp-broadcast-relay/internal/database"
)

// SessionStore provides an in-memory cache backed by the database.
type SessionStore struct {
	mu    sync.RWMutex
	cache map[string]int64 // token -> userID
	db    *database.DB
}

func NewSessionStore(db *database.DB) *SessionStore {
	return &SessionStore{
		cache: make(map[string]int64),
		db:    db,
	}
}

func (s *SessionStore) Get(token string) (int64, bool) {
	s.mu.RLock()
	userID, ok := s.cache[token]
	s.mu.RUnlock()

	if ok {
		return userID, true
	}

	// Check database
	session, err := s.db.GetSessionByToken(token)
	if err != nil || session == nil {
		return 0, false
	}

	// Cache it
	s.mu.Lock()
	s.cache[token] = session.UserID
	s.mu.Unlock()

	return session.UserID, true
}

func (s *SessionStore) Set(token string, userID int64) {
	s.mu.Lock()
	s.cache[token] = userID
	s.mu.Unlock()
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.cache, token)
	s.mu.Unlock()
	s.db.DeleteSessionByToken(token)
}
