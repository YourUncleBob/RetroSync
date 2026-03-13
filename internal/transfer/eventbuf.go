package transfer

import (
	"sync"
	"time"
)

// SyncEvent records a single file transfer event.
type SyncEvent struct {
	Index     int       `json:"index"`
	Time      time.Time `json:"time"`
	Direction string    `json:"direction"` // "in" or "out"
	Group     string    `json:"group"`
	Filename  string    `json:"filename"`
	SizeBytes int64     `json:"size_bytes"`
	Peer      string    `json:"peer"` // server/peer name
}

// EventBuffer holds the last 10 sync events in a ring.
type EventBuffer struct {
	mu      sync.Mutex
	entries []SyncEvent
	next    int
}

// NewEventBuffer creates an empty EventBuffer.
func NewEventBuffer() *EventBuffer {
	return &EventBuffer{}
}

// Append records a new sync event, dropping the oldest when the cap of 10 is exceeded.
func (b *EventBuffer) Append(dir, group, filename, peer string, size int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e := SyncEvent{
		Index:     b.next,
		Time:      time.Now(),
		Direction: dir,
		Group:     group,
		Filename:  filename,
		SizeBytes: size,
		Peer:      peer,
	}
	b.next++
	b.entries = append(b.entries, e)
	if len(b.entries) > 10 {
		b.entries = b.entries[1:]
	}
}

// Since returns all entries with Index > afterIndex.
func (b *EventBuffer) Since(afterIndex int) []SyncEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	var result []SyncEvent
	for _, e := range b.entries {
		if e.Index > afterIndex {
			result = append(result, e)
		}
	}
	return result
}
