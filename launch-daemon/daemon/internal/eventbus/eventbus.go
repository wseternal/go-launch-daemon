// Package eventbus provides a non-blocking publish-subscribe event bus
// for service state changes and log lines.
package eventbus

import (
	"sync"
	"time"
)

// Topic identifies the category of an event.
type Topic string

const (
	TopicState Topic = "state" // Service state changes
	TopicLog   Topic = "log"   // Service log lines
)

// Event is an immutable event published on the bus.
type Event struct {
	Topic     Topic         `json:"topic"`
	Type      string        `json:"type"`           // e.g., "state", "log", "resync"
	Name      string        `json:"name,omitempty"` // service name
	Data      any           `json:"data,omitempty"` // event payload
	Timestamp time.Time     `json:"timestamp"`
}

const (
	subscriberBufSize = 128
)

// Bus is a non-blocking publish-subscribe event bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[*subscriber]struct{}
	closed      bool
}

type subscriber struct {
	ch     chan Event
	topics map[Topic]bool
	drops  int
	mu     sync.Mutex
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{
		subscribers: make(map[*subscriber]struct{}),
	}
}

// Subscribe returns a channel that receives events matching the given topics.
// The caller must call Unsubscribe when done to release resources.
func (b *Bus) Subscribe(topics ...Topic) <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		ch := make(chan Event)
		close(ch)
		return ch
	}

	topicMap := make(map[Topic]bool, len(topics))
	for _, t := range topics {
		topicMap[t] = true
	}

	sub := &subscriber{
		ch:     make(chan Event, subscriberBufSize),
		topics: topicMap,
	}
	b.subscribers[sub] = struct{}{}
	return sub.ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (b *Bus) Unsubscribe(ch <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for sub := range b.subscribers {
		if sub.ch == ch {
			delete(b.subscribers, sub)
			close(sub.ch)
			return
		}
	}
}

// Publish sends an event to all matching subscribers. Non-blocking:
// if a subscriber's buffer is full, the event is dropped and the drop
// counter is incremented.
func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for sub := range b.subscribers {
		if !sub.topics[event.Topic] {
			continue
		}

		// If there were prior drops, prepend a resync marker.
		sub.mu.Lock()
		if sub.drops > 0 {
			resync := Event{
				Topic:     event.Topic,
				Type:      "resync",
				Timestamp: time.Now(),
			}
			// Non-blocking send of resync; if still full, just increment drops.
			select {
			case sub.ch <- resync:
				sub.drops = 0
			default:
				sub.drops++
				sub.mu.Unlock()
				continue
			}
			sub.mu.Unlock()
		} else {
			sub.mu.Unlock()
		}

		// Non-blocking send.
		select {
		case sub.ch <- event:
		default:
			sub.mu.Lock()
			sub.drops++
			sub.mu.Unlock()
		}
	}
}

// Close shuts down the bus and closes all subscriber channels.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	for sub := range b.subscribers {
		close(sub.ch)
		delete(b.subscribers, sub)
	}
}

// DropCount returns the current drop count for a subscriber channel.
// Returns 0 if the channel is not found.
func (b *Bus) DropCount(ch <-chan Event) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for sub := range b.subscribers {
		if sub.ch == ch {
			sub.mu.Lock()
			defer sub.mu.Unlock()
			return sub.drops
		}
	}
	return 0
}
