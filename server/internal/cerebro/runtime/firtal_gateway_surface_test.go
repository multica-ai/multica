package runtime

import "testing"

// TestGatewayIssueSurface locks the channel/DM labeling: a channel/DM is an
// issue with kind 'channel'/'dm', and the gateway spend path must report those
// surfaces instead of folding them into "issue" (TECH-3360). The tokens must
// stay identical to the daemon trace-upload path so both spend systems agree.
func TestGatewayIssueSurface(t *testing.T) {
	cases := map[string]string{
		"channel": "channel",
		"dm":      "dm",
		"issue":   "issue",
		"":        "issue", // unknown / unset kind → ordinary issue
		"task":    "issue",
	}
	for kind, want := range cases {
		if got := gatewayIssueSurface(kind); got != want {
			t.Errorf("gatewayIssueSurface(%q) = %q, want %q", kind, got, want)
		}
	}
}
