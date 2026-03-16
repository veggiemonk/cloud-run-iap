package reqlog

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBuffer_AddAndEntries(t *testing.T) {
	buf := NewBuffer()

	buf.Add(Entry{Method: "GET", Path: "/first"})
	buf.Add(Entry{Method: "POST", Path: "/second"})

	entries := buf.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Newest first.
	if entries[0].Path != "/second" {
		t.Errorf("expected newest first, got: %s", entries[0].Path)
	}
	if entries[1].Path != "/first" {
		t.Errorf("expected oldest last, got: %s", entries[1].Path)
	}
}

func TestBuffer_Empty(t *testing.T) {
	buf := NewBuffer()
	entries := buf.Entries()
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestBuffer_Wraparound(t *testing.T) {
	buf := NewBuffer()

	// Fill beyond capacity to trigger wraparound.
	for i := range maxEntries + 50 {
		buf.Add(Entry{
			Method: "GET",
			Path:   fmt.Sprintf("/%d", i),
		})
	}

	entries := buf.Entries()
	if len(entries) != maxEntries {
		t.Fatalf("expected %d entries after wraparound, got %d", maxEntries, len(entries))
	}

	// Most recent entry should be the last one added.
	expectedPath := fmt.Sprintf("/%d", maxEntries+49)
	if entries[0].Path != expectedPath {
		t.Errorf("expected newest entry %s, got: %s", expectedPath, entries[0].Path)
	}

	// Oldest entry should be the first one that survived the wraparound.
	oldestPath := fmt.Sprintf("/%d", 50)
	if entries[maxEntries-1].Path != oldestPath {
		t.Errorf("expected oldest entry %s, got: %s", oldestPath, entries[maxEntries-1].Path)
	}
}

func TestBuffer_ConcurrentAccess(t *testing.T) {
	buf := NewBuffer()
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := range 10 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 100 {
				buf.Add(Entry{
					Timestamp: time.Now(),
					Method:    "GET",
					Path:      fmt.Sprintf("/worker-%d/%d", id, j),
				})
			}
		}(i)
	}

	// Concurrent readers.
	for range 5 {
		wg.Go(func() {
			for range 100 {
				_ = buf.Entries()
			}
		})
	}

	wg.Wait()

	// After all writes, buffer should be full (1000 writes > maxEntries).
	entries := buf.Entries()
	if len(entries) != maxEntries {
		t.Errorf("expected %d entries, got %d", maxEntries, len(entries))
	}
}

func TestBuffer_EntriesReturnsCopy(t *testing.T) {
	buf := NewBuffer()
	buf.Add(Entry{Method: "GET", Path: "/original"})

	entries := buf.Entries()
	entries[0].Path = "/modified"

	// Verify the buffer is not affected.
	fresh := buf.Entries()
	if fresh[0].Path != "/original" {
		t.Error("Entries() should return a copy, but buffer was modified")
	}
}
