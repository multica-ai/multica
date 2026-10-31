package daemon

import "testing"

func TestContentFilterDoesNotStartAnotherSession(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		"stream disconnected before completion: Incomplete response returned, reason: content_filter",
		`API Error: 400 {"error":{"code":"content_filter"}}`,
	} {
		if freshSessionMayHelp(message) {
			t.Errorf("content filtering must not cause an automatic fresh-session retry: %q", message)
		}
	}
}
