package localmode

import "testing"

func TestResolveListenAddress(t *testing.T) {
	tests := []struct {
		name                       string
		host                       string
		enabled                    bool
		containerLoopbackPublished bool
		want                       string
		wantErr                    bool
	}{
		{name: "standard mode preserves wildcard default", want: ":8080"},
		{name: "local mode defaults to loopback", enabled: true, want: "127.0.0.1:8080"},
		{name: "explicit localhost is accepted", host: "localhost", enabled: true, want: "localhost:8080"},
		{name: "explicit ipv6 loopback is accepted", host: "::1", enabled: true, want: "[::1]:8080"},
		{name: "wildcard is rejected for a direct local run", host: "0.0.0.0", enabled: true, wantErr: true},
		{
			name:                       "container wildcard requires loopback publication attestation",
			host:                       "0.0.0.0",
			enabled:                    true,
			containerLoopbackPublished: true,
			want:                       "0.0.0.0:8080",
		},
		{name: "lan address is rejected even with container attestation", host: "192.168.1.5", enabled: true, containerLoopbackPublished: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveListenAddress("8080", tt.host, tt.enabled, tt.containerLoopbackPublished)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveListenAddress() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ResolveListenAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}
