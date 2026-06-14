package ui

import "sync"

// Broadcaster fans out state-change signals to all connected SSE clients.
// Each subscriber gets a buffered channel of size 1; rapid bursts collapse
// to a single pending signal so the client fetches current state once.
type Broadcaster struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{clients: make(map[chan struct{}]struct{})}
}

// Subscribe returns a receive-only channel and an unsubscribe func.
// The caller must call the returned func when done (typically via defer).
func (b *Broadcaster) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}
}

// Notify signals all current subscribers. Never blocks.
func (b *Broadcaster) Notify() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
