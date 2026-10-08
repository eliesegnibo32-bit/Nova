// Message debouncer (cahier des charges ch. 5.6 — Performance et optimisation
// des coûts).
//
// The debouncer groups rapid-fire messages from the same conversation into a
// single LLM call. Example: a customer who sends "dispo?" + "prix?" + "livraison?"
// in 3 seconds should get ONE response that addresses all three questions,
// not three separate LLM calls.
//
// Usage:
//
//	debouncer := ai.NewDebouncer(4 * time.Second)
//	ch := debouncer.Add(conversationID, "dispo?")
//	// Later, more messages arrive:
//	debouncer.Add(conversationID, "prix?")
//	debouncer.Add(conversationID, "livraison?")
//	// After 4s of silence, ch fires with ["dispo?", "prix?", "livraison?"].
//
// For the simulation console (ch. 5.6 — mode test), the debouncer is BYPASSED
// — each message is processed immediately. The simulation endpoint calls the
// engine directly without going through the debouncer.
package ai

import (
	"sync"
	"time"
)

// Debouncer buffers messages per conversation and fires them as a batch after
// a configurable silence period. The timer resets on each new message.
type Debouncer struct {
	duration time.Duration
	mu       sync.Mutex
	buffers  map[string]*debounceEntry
}

type debounceEntry struct {
	messages []string
	timer    *time.Timer
	// resultChans is the list of channels waiting for the batch. Each Add()
	// call returns a new channel; when the timer fires, the batch is sent on
	// ALL waiting channels (so every caller gets the full batch).
	resultChans []chan []string
}

// NewDebouncer returns a Debouncer with the given silence duration (typically
// 3-5 seconds).
func NewDebouncer(duration time.Duration) *Debouncer {
	if duration <= 0 {
		duration = 4 * time.Second
	}
	return &Debouncer{
		duration: duration,
		buffers:  map[string]*debounceEntry{},
	}
}

// Add buffers a message for the given conversation and returns a channel that
// fires (with all buffered messages for this conversation) when the silence
// period elapses. The channel is buffered (cap 1) so the sender never blocks
// if no one is listening when the timer fires.
func (d *Debouncer) Add(conversationID, msg string) <-chan []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	ch := make(chan []string, 1)
	entry, ok := d.buffers[conversationID]
	if !ok {
		entry = &debounceEntry{}
		d.buffers[conversationID] = entry
	}
	entry.messages = append(entry.messages, msg)
	entry.resultChans = append(entry.resultChans, ch)

	// Reset (or start) the timer.
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.timer = time.AfterFunc(d.duration, func() {
		d.fire(conversationID)
	})

	return ch
}

// fire sends the buffered batch to all waiting channels and removes the entry.
// Called by the timer; never call directly.
func (d *Debouncer) fire(conversationID string) {
	d.mu.Lock()
	entry, ok := d.buffers[conversationID]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.buffers, conversationID)
	d.mu.Unlock()

	// Send the batch to all waiting channels (non-blocking — channels are
	// buffered with cap 1).
	batch := entry.messages
	for _, ch := range entry.resultChans {
		select {
		case ch <- batch:
		default:
			// Channel is full — drop. Shouldn't happen with cap 1.
		}
		close(ch)
	}
}

// Flush forces the batch for a conversation to fire immediately (used in tests
// and for the simulation console if we wanted synchronous behavior).
func (d *Debouncer) Flush(conversationID string) {
	d.fire(conversationID)
}

// Pending returns the number of buffered messages for a conversation (0 if
// none). Useful for diagnostics.
func (d *Debouncer) Pending(conversationID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.buffers[conversationID]; ok {
		return len(e.messages)
	}
	return 0
}
