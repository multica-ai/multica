package main

import "testing"

func TestServerHostIsLocal(t *testing.T) {
	cases := []struct {
		name   string
		server string
		want   bool
	}{
		{"localhost", "http://localhost:8080", true},
		{"127.0.0.1", "http://127.0.0.1:8080", true},
		{"IPv6 loopback", "http://[::1]:8080", true},
		{"LAN IP", "http://192.168.0.28:8080", false},
		{"public FQDN", "https://api.internal.co", false},
		{"unparseable", "://bad", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := serverHostIsLocal(tc.server); got != tc.want {
				t.Errorf("serverHostIsLocal(%q) = %v, want %v", tc.server, got, tc.want)
			}
		})
	}
}

func TestValidateSelfHostURLs(t *testing.T) {
	cases := []struct {
		name      string
		serverURL string
		appURL    string
		wantErr   bool
	}{
		{
			name:      "localhost defaults are valid",
			serverURL: "http://localhost:8080",
			appURL:    "http://localhost:3000",
		},
		{
			name:      "remote split https hosts are valid",
			serverURL: "https://api.example.com",
			appURL:    "https://app.example.com",
		},
		{
			name:      "lan http hosts are valid",
			serverURL: "http://192.168.0.28:8080",
			appURL:    "http://192.168.0.28:3000",
		},
		{
			name:      "remote server with localhost app is rejected",
			serverURL: "https://api.example.com",
			appURL:    "http://localhost:3000",
			wantErr:   true,
		},
		{
			name:      "https app with remote http server is rejected",
			serverURL: "http://api.example.com",
			appURL:    "https://app.example.com",
			wantErr:   true,
		},
		{
			name:      "invalid server URL is rejected",
			serverURL: "api.example.com",
			appURL:    "https://app.example.com",
			wantErr:   true,
		},
		{
			name:      "invalid app URL is rejected",
			serverURL: "https://api.example.com",
			appURL:    "ftp://app.example.com",
			wantErr:   true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validateSelfHostURLs(tc.serverURL, tc.appURL)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateSelfHostURLs(%q, %q) error = %v, wantErr %v", tc.serverURL, tc.appURL, err, tc.wantErr)
			}
		})
	}
}
