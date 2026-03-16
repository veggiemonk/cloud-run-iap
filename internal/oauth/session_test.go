package oauth

import (
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := NewSessionStore(nil)
	token := &oauth2.Token{AccessToken: "test-token"}

	session := store.Create("user@example.com", "Test User", "pic.jpg", token)

	if session.ID == "" {
		t.Fatal("expected session ID to be set")
	}
	if session.Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %s", session.Email)
	}

	got := store.Get(session.ID)
	if got == nil {
		t.Fatal("expected to retrieve session")
	}
	if got.Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %s", got.Email)
	}
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore(nil)
	session := store.Create("user@example.com", "Test User", "", nil)

	store.Delete(session.ID)

	if got := store.Get(session.ID); got != nil {
		t.Error("expected session to be deleted")
	}
}

func TestSessionStore_GetNonExistent(t *testing.T) {
	store := NewSessionStore(nil)
	if got := store.Get("nonexistent"); got != nil {
		t.Error("expected nil for nonexistent session")
	}
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	store := NewSessionStore(nil)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := store.Create("user@example.com", "User", "", nil)
			store.Get(s.ID)
			store.Delete(s.ID)
		}()
	}

	wg.Wait()
}

func TestSessionStore_Cleanup(t *testing.T) {
	store := NewSessionStore(nil)

	// Create a session and backdate it beyond the TTL.
	session := store.Create("old@example.com", "Old User", "", nil)
	store.mu.Lock()
	store.sessions[session.ID].CreatedAt = time.Now().Add(-(sessionTTL + time.Hour))
	store.mu.Unlock()

	// Create a fresh session.
	fresh := store.Create("new@example.com", "New User", "", nil)

	// Run cleanup inline (same logic as StartCleanup ticker).
	store.mu.Lock()
	for id, sess := range store.sessions {
		if time.Since(sess.CreatedAt) > sessionTTL {
			delete(store.sessions, id)
		}
	}
	store.mu.Unlock()

	if got := store.Get(session.ID); got != nil {
		t.Error("expected old session to be evicted")
	}
	if got := store.Get(fresh.ID); got == nil {
		t.Error("expected fresh session to still exist")
	}
}

func TestSessionStore_MaxSessions(t *testing.T) {
	store := NewSessionStore(nil)

	// Fill to capacity.
	for i := 0; i < maxSessions; i++ {
		store.Create("user@example.com", "User", "", nil)
	}

	if store.Len() != maxSessions {
		t.Fatalf("expected %d sessions, got %d", maxSessions, store.Len())
	}

	// Adding one more should evict the oldest and stay at cap.
	store.Create("new@example.com", "New User", "", nil)

	if store.Len() != maxSessions {
		t.Errorf("expected %d sessions after overflow, got %d", maxSessions, store.Len())
	}
}
