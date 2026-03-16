package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const (
	maxSessions = 1000
	sessionTTL  = 24 * time.Hour
)

// Session represents an authenticated user session.
type Session struct {
	ID        string
	Email     string
	Name      string
	Picture   string
	Token     *oauth2.Token
	CreatedAt time.Time
}

// SessionStore is a thread-safe in-memory session store.
type SessionStore struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	OAuthConfig *oauth2.Config
}

// NewSessionStore creates a new empty session store.
func NewSessionStore(cfg *oauth2.Config) *SessionStore {
	return &SessionStore{
		sessions:    make(map[string]*Session),
		OAuthConfig: cfg,
	}
}

// StartCleanup launches a background goroutine that periodically evicts expired sessions.
func (s *SessionStore) StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			s.mu.Lock()
			for id, sess := range s.sessions {
				if time.Since(sess.CreatedAt) > sessionTTL {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}()
}

// Create creates a new session and returns it.
// If the store is at capacity, the oldest session is evicted first.
func (s *SessionStore) Create(email, name, picture string, token *oauth2.Token) *Session {
	id := generateID()
	session := &Session{
		ID:        id,
		Email:     email,
		Name:      name,
		Picture:   picture,
		Token:     token,
		CreatedAt: time.Now(),
	}
	s.mu.Lock()
	if len(s.sessions) >= maxSessions {
		s.evictOldest()
	}
	s.sessions[id] = session
	s.mu.Unlock()
	return session
}

// evictOldest removes the oldest session. Must be called with s.mu held.
func (s *SessionStore) evictOldest() {
	var oldestID string
	var oldestTime time.Time
	for id, sess := range s.sessions {
		if oldestID == "" || sess.CreatedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = sess.CreatedAt
		}
	}
	if oldestID != "" {
		delete(s.sessions, oldestID)
	}
}

// Get retrieves a session by ID, or nil if not found.
func (s *SessionStore) Get(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

// Delete removes a session by ID.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// Len returns the number of active sessions.
func (s *SessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// generateID generates a 32-byte random hex-encoded string.
func generateID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
