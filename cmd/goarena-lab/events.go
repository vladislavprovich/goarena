package main

import (
	"encoding/json"
	"sync"
	"time"
)

// Event types sent over /api/events.
const (
	evSessionStarted  = "session_started"
	evRunStarted      = "run_started"
	evProgress        = "progress"
	evLog             = "log"
	evRunFinished     = "run_finished"
	evKnees           = "knees"
	evSessionFinished = "session_finished"
	evBench           = "bench"
)

// Event is one server-sent event. Data depends on Type.
type Event struct {
	Seq  int64     `json:"seq"`
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Data any       `json:"data,omitempty"`
}

// encodedEvent is an event serialized once for all subscribers.
type encodedEvent struct {
	seq  int64
	typ  string
	data []byte
}

// Hub fans events out to SSE subscribers and keeps the events of the
// current session, so that a browser tab that connects mid-run (or
// reconnects with Last-Event-ID) is replayed the whole session.
//
// Sequence numbers start at the current Unix time in microseconds, so they
// keep growing across restarts of the lab and a stale Last-Event-ID never
// suppresses the replay of a new session.
type Hub struct {
	mu       sync.Mutex
	seq      int64
	buf      []encodedEvent
	logs     int // log events in buf
	subs     map[*subscriber]struct{}
	closed   bool
	maxBuf   int
	maxLogs  int
	subQueue int
}

type subscriber struct {
	ch chan encodedEvent
}

func newHub() *Hub {
	return &Hub{
		seq:      time.Now().UnixMicro(),
		subs:     map[*subscriber]struct{}{},
		maxBuf:   200_000,
		maxLogs:  5_000,
		subQueue: 4096,
	}
}

// Reset clears the replay buffer (a new session starts).
func (h *Hub) Reset() {
	h.mu.Lock()
	h.buf, h.logs = nil, 0
	h.mu.Unlock()
}

// Publish sends an event to every subscriber and buffers it for replay.
// Log events beyond maxLogs are broadcast but not buffered. A subscriber
// whose queue is full is disconnected; its browser reconnects with
// Last-Event-ID and is replayed what it missed.
func (h *Hub) Publish(typ string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.seq++
	b, err := json.Marshal(Event{Seq: h.seq, Type: typ, Time: time.Now(), Data: data})
	if err != nil {
		b, _ = json.Marshal(Event{Seq: h.seq, Type: evLog, Time: time.Now(),
			Data: map[string]any{"index": -1, "line": "lab: cannot encode event: " + err.Error()}})
	}
	ev := encodedEvent{seq: h.seq, typ: typ, data: b}
	buffer := len(h.buf) < h.maxBuf
	if typ == evLog {
		buffer = buffer && h.logs < h.maxLogs
		if buffer {
			h.logs++
		}
	}
	if buffer {
		h.buf = append(h.buf, ev)
	}
	for s := range h.subs {
		select {
		case s.ch <- ev:
		default:
			close(s.ch)
			delete(h.subs, s)
		}
	}
}

// Subscribe registers a subscriber and returns the buffered events with a
// sequence number above after. Events published later arrive on the
// subscriber's channel, without gaps or duplicates.
func (h *Hub) Subscribe(after int64) ([]encodedEvent, *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := &subscriber{ch: make(chan encodedEvent, h.subQueue)}
	if h.closed {
		close(s.ch)
		return nil, s
	}
	var replay []encodedEvent
	for _, ev := range h.buf {
		if ev.seq > after {
			replay = append(replay, ev)
		}
	}
	h.subs[s] = struct{}{}
	return replay, s
}

// Unsubscribe removes a subscriber.
func (h *Hub) Unsubscribe(s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; ok {
		delete(h.subs, s)
		close(s.ch)
	}
}

// Close disconnects all subscribers; later Publish calls are dropped.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.subs {
		close(s.ch)
		delete(h.subs, s)
	}
}

// Subscribers returns the number of connected subscribers.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
