package goserver

import "testing"

func TestNewServerNormalizesPortAddress(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "port", addr: "8080", want: ":8080"},
		{name: "port with colon", addr: ":8080", want: ":8080"},
		{name: "port with repeated colons", addr: "::8080", want: ":8080"},
		{name: "automatic port", addr: "0", want: ":0"},
		{name: "ipv4", addr: "127.0.0.1:8080", want: "127.0.0.1:8080"},
		{name: "hostname", addr: "localhost:8080", want: "localhost:8080"},
		{name: "ipv6", addr: "[::1]:8080", want: "[::1]:8080"},
		{name: "empty", addr: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewServer(tt.addr)
			if s.addr != tt.want {
				t.Fatalf("Server.addr = %q, want %q", s.addr, tt.want)
			}
			if s.srv.Addr != tt.want {
				t.Fatalf("http.Server.Addr = %q, want %q", s.srv.Addr, tt.want)
			}
		})
	}
}
