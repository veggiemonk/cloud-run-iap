package reqlog

import (
	"sync"
	"time"
)

const maxEntries = 200

// Entry represents a single logged request.
type Entry struct {
	Timestamp  time.Time `json:"timestamp"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Email      string    `json:"email,omitempty"`
	AuthType   string    `json:"auth_type,omitempty"`
	RemoteAddr string    `json:"remote_addr"`
}

// Buffer is a thread-safe ring buffer for request log entries.
type Buffer struct {
	mu      sync.RWMutex
	entries []Entry
	pos     int
	full    bool
}

// NewBuffer creates a new ring buffer.
func NewBuffer() *Buffer {
	return &Buffer{
		entries: make([]Entry, maxEntries),
	}
}

// Add inserts an entry into the ring buffer.
func (b *Buffer) Add(e Entry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.entries[b.pos] = e
	b.pos++
	if b.pos >= maxEntries {
		b.pos = 0
		b.full = true
	}
}

// Entries returns a copy of all entries, newest first.
func (b *Buffer) Entries() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var count int
	if b.full {
		count = maxEntries
	} else {
		count = b.pos
	}

	result := make([]Entry, count)
	for i := 0; i < count; i++ {
		// Walk backwards from the most recent entry.
		idx := (b.pos - 1 - i + maxEntries) % maxEntries
		result[i] = b.entries[idx]
	}
	return result
}
