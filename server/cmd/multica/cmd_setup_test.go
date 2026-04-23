package main

import "testing"

func TestIsLocalhostURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"http://localhost:8080", true},
		{"http://localhost", true},
		{"https://127.0.0.1:8080", true},
		{"http://[::1]:8080", true},
		{"http://api.example.com", false},
		{"https://app.internal.co:8080", false},
		{"http://0.0.0.0:8080", false},
		{"", false},
		{"not a url", false},
	}
	for _, c := range cases {
		if got := isLocalhostURL(c.in); got != c.want {
			t.Errorf("isLocalhostURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
