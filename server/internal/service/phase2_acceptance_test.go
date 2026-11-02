package service

// Phase 2 acceptance, clock-dependent part of spec 2.10 bullet 4. The 08:00
// publish and the catch-up before 10:00 use the job tests' clock seams, so they
// live here. The Telegram round trip is in the handler package, which can import
// the Telegram adapter without a cycle.

import (
	"testing"
	"time"
)

// A brief published at 08:00 local time is queued for a push-enabled member, and
// the push job sends it at once through the chat app's notifier.
func TestPhase2Acceptance_Bullet4_BriefPublishedAt0800ReachesTheChatAppAtOnce(t *testing.T) {
	f := newBriefPushFixture(t)
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	f.pointClock(time.Date(2026, 1, 10, 8, 0, 0, 0, loc))

	userID, bindingID := f.member(nil)
	f.preference(userID, true, bindingID, nil, nil)
	proposal := f.proposal("Acceptance 08:00 decision", nil)
	briefID := f.brief("Morning headline", proposal)

	if n := f.enqueue(briefID); n != 1 {
		t.Fatalf("enqueued %d deliveries, want 1 for the one opted-in member", n)
	}
	if got := f.delivery(briefID, userID); got.State != "pending" {
		t.Fatalf("a queued brief starts pending, got %s", got.State)
	}
	if n := f.deliverDue(); n < 1 {
		t.Fatalf("the push job sent %d, want the 08:00 brief", n)
	}
	if got := f.delivery(briefID, userID); got.State != "sent" {
		t.Fatalf("delivery state = %s, want sent within the same pass", got.State)
	}
	if calls := f.notifier.calls.Load(); calls != 1 {
		t.Fatalf("the notifier was called %d times, want once", calls)
	}
	sent := f.notifier.sent[0]
	if sent.binding.ChannelType != briefPushTestChat || sent.msg.Text == "" {
		t.Fatalf("sent %+v, want the Telegram binding and the message text", sent)
	}
}

// A machine asleep at 08:00: the brief is missing, the runtime stays offline,
// and once it is back the catch-up job produces the brief at the 10:00 pass.
func TestPhase2Acceptance_Bullet4_MissedBriefIsCaughtUpAtTheTenOClockPass(t *testing.T) {
	f := newBriefCatchupFixture(t)

	f.setRuntimeStatus("offline")
	f.at(8, 0)
	f.pass()
	f.at(9, 30)
	f.pass()
	if f.firedHere() != 0 {
		t.Fatalf("fired while the runtime was offline: %d", f.firedHere())
	}

	f.setRuntimeStatus("online")
	f.at(9, 59)
	f.pass()
	if f.firedHere() != 0 {
		t.Fatalf("fired at 09:59, before the 10:00 catch-up hour")
	}

	f.at(10, 0)
	f.pass()
	if f.firedHere() != 1 {
		t.Fatalf("fired %d times at the 10:00 pass, want once", f.firedHere())
	}
	if f.disp.sources[0] != "catchup" {
		t.Fatalf("source = %q, want catchup", f.disp.sources[0])
	}
}
