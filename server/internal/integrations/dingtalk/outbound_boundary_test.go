package dingtalk

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSender_EmojiReactionCredentialAndTerminalFailures(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		robot                       string
		tokenStatus, reactionStatus int
		wantToken, wantReaction     int
	}{
		{"missing robot", "", 200, 200, 0, 0},
		{"token failure", "robot", 500, 200, 1, 0},
		{"retry exhausted", "robot", 200, 401, 2, 2},
		{"forbidden", "robot", 200, 403, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tokenCalls, reactionCalls int
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				status, body := tc.reactionStatus, `{"success":true}`
				if r.URL.Path == accessTokenPath {
					tokenCalls++
					status = tc.tokenStatus
					body = `{"accessToken":"token","expireIn":7200}`
				} else {
					reactionCalls++
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}, "https://dingtalk.test")
			s := newTestSender(client)
			s.robotCode = tc.robot
			err := s.setEmojiReaction(context.Background(), sendTarget{ConversationID: "cid", SourceMessageID: "source"}, emotionAcknowledged, false)
			if err == nil || tokenCalls != tc.wantToken || reactionCalls != tc.wantReaction {
				t.Fatalf("error=%v token=%d reaction=%d", err, tokenCalls, reactionCalls)
			}
		})
	}
}
