package antivpn

import (
	"context"
	"log/slog"
	"sync"
)

// EventHandler is the contract for any consumer that wants to receive
// parsed events from the supervisor's central dispatcher. Each handler
// runs on its own goroutine; implementations therefore do NOT need to
// be safe for concurrent calls into HandleEvent, but they MUST treat
// the supplied context as a hard cancellation signal so the supervisor
// can shut them down quickly when the dedicated server exits.
type EventHandler interface {
	// Name is used in diagnostic logs to identify the handler when its
	// queue overflows or it is removed.
	Name() string
	// HandleEvent processes one parsed event. The handler is expected
	// to be reasonably fast; slow handlers will see their oldest queued
	// events dropped under the dispatcher's drop-oldest policy.
	HandleEvent(ctx context.Context, event Event)
}

// EventDispatchPolicy controls what the dispatcher does when a
// handler's per-handler queue is full at publish time.
type EventDispatchPolicy int

const (
	// EventDispatchDropOldest evicts the oldest queued event for the
	// slow handler and enqueues the new one. This bounds memory use,
	// keeps the supervisor's scanner non-blocking, and biases the
	// retained backlog toward the most recent state of the world (which
	// is what addons typically want when they fall behind).
	EventDispatchDropOldest EventDispatchPolicy = iota
	// EventDispatchDropNewest discards the incoming event when the
	// queue is full. Provided for completeness and for addons that
	// strictly prefer ordered delivery of older events; the supervisor
	// default is drop-oldest.
	EventDispatchDropNewest
)

// ParseEventDispatchPolicy converts the operator-facing
// ADDON_EVENT_BUS_DROP_POLICY string into the internal enum. Unknown
// values fall back to drop-oldest, matching the documented default.
func ParseEventDispatchPolicy(value string) EventDispatchPolicy {
	switch value {
	case "drop-newest":
		return EventDispatchDropNewest
	case "", "drop-oldest":
		return EventDispatchDropOldest
	default:
		return EventDispatchDropOldest
	}
}

// EventDispatcher fans out parsed events to a set of registered
// handlers. The dispatcher is intentionally simple:
//
//   - Each handler has its own buffered channel with a fixed capacity.
//   - Publish() never blocks the caller; if a handler queue is full the
//     drop policy is applied.
//   - Each handler runs on a dedicated goroutine; one slow or crashed
//     handler cannot stall the supervisor's stdout/stderr scanner.
//
// The dispatcher does not deduplicate or replay events. The supervisor
// publishes each parsed line exactly once.
type EventDispatcher struct {
	logger     *slog.Logger
	bufferSize int
	policy     EventDispatchPolicy

	mu       sync.Mutex
	handlers []*dispatcherEntry
	closed   bool
}

type dispatcherEntry struct {
	handler  EventHandler
	queue    chan Event
	dropMu   sync.Mutex
	dropped  uint64
	doneOnce sync.Once
	done     chan struct{}
}

// NewEventDispatcher constructs a dispatcher. bufferSize is the
// per-handler channel capacity; values <= 0 fall back to a small
// default that is still large enough for normal map cycles. The logger
// receives a single warning the first time a given handler starts
// dropping events under load.
func NewEventDispatcher(logger *slog.Logger, bufferSize int, policy EventDispatchPolicy) *EventDispatcher {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &EventDispatcher{
		logger:     logger,
		bufferSize: bufferSize,
		policy:     policy,
	}
}

// Subscribe registers a handler with the dispatcher and starts a
// goroutine that drains its queue. The returned cleanup function stops
// the goroutine and removes the handler.
func (d *EventDispatcher) Subscribe(ctx context.Context, handler EventHandler) func() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return func() {}
	}

	entry := &dispatcherEntry{
		handler: handler,
		queue:   make(chan Event, d.bufferSize),
		done:    make(chan struct{}),
	}
	d.handlers = append(d.handlers, entry)
	d.mu.Unlock()

	go d.runHandler(ctx, entry)

	return func() {
		d.removeEntry(entry)
	}
}

func (d *EventDispatcher) runHandler(ctx context.Context, entry *dispatcherEntry) {
	defer entry.doneOnce.Do(func() { close(entry.done) })

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-entry.queue:
			if !ok {
				return
			}
			func() {
				defer func() {
					if r := recover(); r != nil && d.logger != nil {
						d.logger.Warn(
							"event handler panicked; continuing without it",
							"handler", entry.handler.Name(),
							"panic", r,
						)
					}
				}()
				entry.handler.HandleEvent(ctx, ev)
			}()
		}
	}
}

// Publish fans an event out to every registered handler. It never
// blocks: if a handler queue is full the configured drop policy is
// applied. Returns the number of handlers that received (or had
// queued) the event, which is useful in tests.
func (d *EventDispatcher) Publish(event Event) int {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return 0
	}
	// Snapshot under the lock so handlers added concurrently do not see
	// a partial fan-out, but the actual sends happen without holding
	// the dispatcher lock so a slow handler cannot stall Publish.
	entries := make([]*dispatcherEntry, len(d.handlers))
	copy(entries, d.handlers)
	d.mu.Unlock()

	delivered := 0
	for _, entry := range entries {
		if d.deliver(entry, event) {
			delivered++
		}
	}
	return delivered
}

func (d *EventDispatcher) deliver(entry *dispatcherEntry, event Event) bool {
	select {
	case entry.queue <- event:
		return true
	default:
	}

	switch d.policy {
	case EventDispatchDropNewest:
		d.recordDrop(entry)
		return false
	default:
		// drop-oldest: pop one queued event then enqueue the new one.
		// Both operations are non-blocking; if a competing receiver
		// drained the queue between attempts we still succeed below.
		entry.dropMu.Lock()
		select {
		case <-entry.queue:
		default:
		}
		select {
		case entry.queue <- event:
			entry.dropMu.Unlock()
			d.recordDrop(entry)
			return true
		default:
			entry.dropMu.Unlock()
			d.recordDrop(entry)
			return false
		}
	}
}

func (d *EventDispatcher) recordDrop(entry *dispatcherEntry) {
	entry.dropMu.Lock()
	first := entry.dropped == 0
	entry.dropped++
	count := entry.dropped
	entry.dropMu.Unlock()
	if first && d.logger != nil {
		d.logger.Warn(
			"event handler queue overflowed; applying drop policy",
			"handler", entry.handler.Name(),
			"buffer_size", d.bufferSize,
			"policy_drop_count", count,
		)
	}
}

func (d *EventDispatcher) removeEntry(entry *dispatcherEntry) {
	d.mu.Lock()
	for i, e := range d.handlers {
		if e == entry {
			d.handlers = append(d.handlers[:i], d.handlers[i+1:]...)
			break
		}
	}
	d.mu.Unlock()

	close(entry.queue)
	<-entry.done
}

// Close removes all handlers and stops their goroutines. After Close,
// Publish becomes a no-op. Safe to call multiple times.
func (d *EventDispatcher) Close() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	entries := d.handlers
	d.handlers = nil
	d.mu.Unlock()

	for _, entry := range entries {
		close(entry.queue)
		<-entry.done
	}
}
