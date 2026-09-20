package handler

import "testing"

func TestValidExpoPushToken(t *testing.T) {
	for _, token := range []string{
		"ExpoPushToken[abc123]",
		"ExponentPushToken[abc123]",
	} {
		if !validExpoPushToken(token) {
			t.Fatalf("validExpoPushToken(%q) = false", token)
		}
	}
	for _, token := range []string{"", "abc123", "ExpoPushToken[", "ExpoPushToken[abc123"} {
		if validExpoPushToken(token) {
			t.Fatalf("validExpoPushToken(%q) = true", token)
		}
	}
}
