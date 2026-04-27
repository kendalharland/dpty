package dpty

import (
	"bytes"
	"testing"
	"time"
)

func newTestSession(bufMax int) *Session {
	return &Session{
		Alias:     "test",
		Shell:     "bash",
		CreatedAt: time.Now(),
		bufMax:    bufMax,
	}
}

func TestSession_appendOutput_capsBuffer(t *testing.T) {
	s := newTestSession(8)
	s.appendOutput([]byte("12345"))
	s.appendOutput([]byte("ABCDE"))
	snap, _, _ := s.subscribe()
	if string(snap) != "345ABCDE" {
		t.Fatalf("buffer = %q, want %q", snap, "345ABCDE")
	}
	if len(snap) > 8 {
		t.Fatalf("buffer exceeded cap: len=%d", len(snap))
	}
}

func TestSession_subscribe_replaysSnapshotAndStreams(t *testing.T) {
	s := newTestSession(0)
	s.appendOutput([]byte("hello "))

	snap, ch, closed := s.subscribe()
	if closed {
		t.Fatal("session unexpectedly closed")
	}
	if !bytes.Equal(snap, []byte("hello ")) {
		t.Errorf("snapshot = %q, want %q", snap, "hello ")
	}

	s.appendOutput([]byte("world"))
	select {
	case got := <-ch:
		if !bytes.Equal(got, []byte("world")) {
			t.Errorf("streamed chunk = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no chunk received from subscription")
	}
}

func TestSession_closeOutput_closesSubscribers(t *testing.T) {
	s := newTestSession(0)
	_, ch, _ := s.subscribe()
	s.closeOutput()
	if _, ok := <-ch; ok {
		t.Fatal("expected channel closed")
	}
	_, ch2, alreadyClosed := s.subscribe()
	if !alreadyClosed {
		t.Fatal("expected alreadyClosed=true after closeOutput")
	}
	if ch2 != nil {
		t.Fatal("expected nil channel after closeOutput")
	}
}

func TestSession_tryAcquireAttach_isExclusive(t *testing.T) {
	s := newTestSession(0)
	if !s.tryAcquireAttach() {
		t.Fatal("first acquire should succeed")
	}
	if s.tryAcquireAttach() {
		t.Fatal("second acquire should fail while attached")
	}
	if !s.IsAttached() {
		t.Fatal("IsAttached should be true")
	}
	s.setAttached(false)
	if s.IsAttached() {
		t.Fatal("IsAttached should be false after setAttached(false)")
	}
	if !s.tryAcquireAttach() {
		t.Fatal("acquire should succeed after release")
	}
}
