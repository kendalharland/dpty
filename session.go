package dpty

import (
	"os"
	"sync"
	"time"
)

// DefaultSessionBufferMax is the default per-session scrollback cap (in
// bytes) that [Server] uses when constructing sessions. Reattaching
// clients are replayed up to this many bytes of prior PTY output.
const DefaultSessionBufferMax = 1 << 20 // 1 MiB

// Session represents one live PTY managed by a [Server].
//
// Sessions are constructed by [Server.CreateSession]. Their fields are
// exported only for read-only use (e.g., to build [SessionInfo] values).
// Mutating them externally is undefined.
type Session struct {
	Alias     string
	Shell     string
	Args      []string
	CreatedAt time.Time

	pty     *os.File
	process *os.Process
	bufMax  int

	bufMu  sync.Mutex
	buffer []byte
	subs   map[chan []byte]struct{}
	closed bool

	attachMu sync.Mutex
	attached bool
}

// IsAttached reports whether a client is currently attached.
func (s *Session) IsAttached() bool {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	return s.attached
}

// Info returns the public-facing snapshot of this session.
func (s *Session) Info() SessionInfo {
	return SessionInfo{
		Alias:     s.Alias,
		Shell:     s.Shell,
		Args:      append([]string{}, s.Args...),
		CreatedAt: s.CreatedAt,
		InUse:     s.IsAttached(),
	}
}

// appendOutput records data to the scrollback buffer (truncating to
// bufMax) and forwards a copy to every active subscriber. Slow
// subscribers have their chunk dropped; they will resync from the
// snapshot on the next attach.
func (s *Session) appendOutput(data []byte) {
	cp := append([]byte{}, data...)
	s.bufMu.Lock()
	s.buffer = append(s.buffer, cp...)
	if s.bufMax > 0 && len(s.buffer) > s.bufMax {
		s.buffer = s.buffer[len(s.buffer)-s.bufMax:]
	}
	for ch := range s.subs {
		select {
		case ch <- cp:
		default:
		}
	}
	s.bufMu.Unlock()
}

// subscribe returns a snapshot of buffered output and a channel that
// receives subsequent chunks. If the session has already been closed,
// snapshot is still returned but ch is nil and alreadyClosed is true.
func (s *Session) subscribe() (snapshot []byte, ch chan []byte, alreadyClosed bool) {
	s.bufMu.Lock()
	defer s.bufMu.Unlock()
	snapshot = append([]byte{}, s.buffer...)
	if s.closed {
		return snapshot, nil, true
	}
	if s.subs == nil {
		s.subs = map[chan []byte]struct{}{}
	}
	ch = make(chan []byte, 64)
	s.subs[ch] = struct{}{}
	return snapshot, ch, false
}

func (s *Session) unsubscribe(ch chan []byte) {
	s.bufMu.Lock()
	if _, ok := s.subs[ch]; ok {
		delete(s.subs, ch)
		close(ch)
	}
	s.bufMu.Unlock()
}

// closeOutput marks the session closed and closes every subscriber
// channel so attach loops exit cleanly.
func (s *Session) closeOutput() {
	s.bufMu.Lock()
	s.closed = true
	for ch := range s.subs {
		close(ch)
		delete(s.subs, ch)
	}
	s.bufMu.Unlock()
}

func (s *Session) setAttached(v bool) {
	s.attachMu.Lock()
	s.attached = v
	s.attachMu.Unlock()
}

// tryAcquireAttach atomically marks the session as attached if no client
// currently holds it. It returns true if the caller now owns the slot.
func (s *Session) tryAcquireAttach() bool {
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	if s.attached {
		return false
	}
	s.attached = true
	return true
}
