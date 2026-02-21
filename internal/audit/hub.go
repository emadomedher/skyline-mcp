package audit

import "sync"

// Hub broadcasts audit events to live subscribers (e.g. SSE admin feed).
type Hub struct {
	mu   sync.RWMutex
	subs map[uint64]chan Event
	next uint64
}

// NewHub creates a new event hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[uint64]chan Event)}
}

// Subscribe returns a subscriber ID and a channel that receives events.
func (h *Hub) Subscribe() (uint64, <-chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.next
	h.next++
	ch := make(chan Event, 64)
	h.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (h *Hub) Unsubscribe(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subs[id]; ok {
		close(ch)
		delete(h.subs, id)
	}
}

// Publish sends an event to all subscribers. Non-blocking: if a subscriber's
// buffer is full the event is dropped for that subscriber.
func (h *Hub) Publish(event Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

// GenericHub broadcasts any-typed events to live subscribers.
// Used for agent lifecycle events (session connect/disconnect, tool start/end).
type GenericHub struct {
	mu   sync.RWMutex
	subs map[uint64]chan any
	next uint64
}

// NewGenericHub creates a new generic event hub.
func NewGenericHub() *GenericHub {
	return &GenericHub{subs: make(map[uint64]chan any)}
}

// Subscribe returns a subscriber ID and a channel that receives events.
func (h *GenericHub) Subscribe() (uint64, <-chan any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.next
	h.next++
	ch := make(chan any, 64)
	h.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (h *GenericHub) Unsubscribe(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subs[id]; ok {
		close(ch)
		delete(h.subs, id)
	}
}

// Publish sends an event to all subscribers. Non-blocking.
func (h *GenericHub) Publish(event any) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
