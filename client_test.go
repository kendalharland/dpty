package dpty

import "testing"

func TestAttachWebSocketURL(t *testing.T) {
	cases := []struct {
		addr, alias, want string
	}{
		{"http://localhost:5137", "abc", "ws://localhost:5137/abc"},
		{"http://localhost:5137/", "abc", "ws://localhost:5137/abc"},
		{"https://example.com", "x", "wss://example.com/x"},
		{"localhost:5137", "y", "localhost:5137/y"}, // unchanged scheme
	}
	for _, tc := range cases {
		if got := AttachWebSocketURL(tc.addr, tc.alias); got != tc.want {
			t.Errorf("AttachWebSocketURL(%q, %q) = %q, want %q", tc.addr, tc.alias, got, tc.want)
		}
	}
}
