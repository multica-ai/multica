package wecom

import "time"

// Package tests wait 400ms rather than the production ackTimeout for a verdict.
// It stays that long for everyone because some fakes answer from another
// goroutine — the channel's read loop, a held upload chunk — and a verdict on
// its way must not lose to the timer on a loaded race-detector run.
func init() {
	newWSSenderAckTimeout = 400 * time.Millisecond
}

// lostAckTimeout is for a sender whose only request is one its fake connection
// never answers. With no verdict ever on its way there is nothing for a short
// timer to race, so such a test need not wait out the package-wide one above.
const lostAckTimeout = 10 * time.Millisecond
