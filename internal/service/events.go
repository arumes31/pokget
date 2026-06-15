// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package service

import (
	"sync"
)

// Event represents a system event.
type Event struct {
	Type    string
	Payload interface{}
}

// subscriberEntry tracks a subscriber channel and its unsubscribe function.
type subscriberEntry struct {
	ch chan Event
}

// EventBus is a simple internal event bus using Go channels.
type EventBus struct {
	subscribers map[string][]subscriberEntry
	mu          sync.RWMutex
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]subscriberEntry),
	}
}

// Subscribe adds a subscriber for a specific event type.
// Returns the channel to receive events on and an unsubscribe function.
// BUG-H09 FIX: Added unsubscribe mechanism to prevent goroutine leaks.
func (b *EventBus) Subscribe(eventType string) (chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, 10)
	entry := subscriberEntry{ch: ch}
	b.subscribers[eventType] = append(b.subscribers[eventType], entry)

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		subs := b.subscribers[eventType]
		for i, s := range subs {
			if s.ch == ch {
				// Remove this subscriber
				b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
				close(ch)
				return
			}
		}
	}

	return ch, unsubscribe
}

// Publish broadcasts an event to all subscribers of the given type.
// BUG-H09 FIX: Use select with default to avoid blocking goroutines
// when a subscriber's channel is full, preventing goroutine leaks.
func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, entry := range b.subscribers[event.Type] {
		select {
		case entry.ch <- event:
			// Event sent successfully
		default:
			// Channel is full, drop the event to avoid blocking/goroutine leak
		}
	}
}
