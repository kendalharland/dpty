package dpty

import "testing"

func TestRewriteLoopbackAddress(t *testing.T) {
	cases := []struct {
		name      string
		addr      string
		peer      string
		want      string
		rewritten bool
	}{
		{
			name:      "localhost rewritten to peer ipv4",
			addr:      "http://localhost:5137",
			peer:      "10.0.0.5",
			want:      "http://10.0.0.5:5137",
			rewritten: true,
		},
		{
			name:      "127.0.0.1 rewritten",
			addr:      "http://127.0.0.1:5137",
			peer:      "192.168.1.20",
			want:      "http://192.168.1.20:5137",
			rewritten: true,
		},
		{
			name:      "ipv6 loopback rewritten",
			addr:      "http://[::1]:5137",
			peer:      "10.0.0.5",
			want:      "http://10.0.0.5:5137",
			rewritten: true,
		},
		{
			name:      "ipv6 peer wraps in brackets",
			addr:      "http://localhost:5137",
			peer:      "fe80::1",
			want:      "http://[fe80::1]:5137",
			rewritten: true,
		},
		{
			name:      "non-loopback advertise left alone",
			addr:      "http://my-host.example:5137",
			peer:      "10.0.0.5",
			rewritten: false,
		},
		{
			name:      "loopback peer is a no-op",
			addr:      "http://localhost:5137",
			peer:      "127.0.0.1",
			rewritten: false,
		},
		{
			name:      "empty peer is a no-op",
			addr:      "http://localhost:5137",
			peer:      "",
			rewritten: false,
		},
		{
			name:      "https preserved",
			addr:      "https://localhost:5137",
			peer:      "10.0.0.5",
			want:      "https://10.0.0.5:5137",
			rewritten: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := rewriteLoopbackAddress(tc.addr, tc.peer)
			if ok != tc.rewritten {
				t.Fatalf("rewritten = %v, want %v (got=%q)", ok, tc.rewritten, got)
			}
			if ok && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
