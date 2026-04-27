package dpty

import (
	"strings"
	"testing"
)

func TestIsValidSessionName(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"a", true},
		{"ABC", true},
		{"123", true},
		{"my-session.1", true},
		{"under_score", true},
		{"has space", false},
		{"slash/inside", false},
		{"colon:bad", false},
		{"emoji😀", false},
		{strings.Repeat("a", MaxSessionNameLen), true},
		{strings.Repeat("a", MaxSessionNameLen+1), false},
	}
	for _, tc := range cases {
		if got := IsValidSessionName(tc.in); got != tc.want {
			t.Errorf("IsValidSessionName(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
