package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

type fakeMobileLogs struct {
	phones []MobilePhone
	reads  []string
	fail   error
}

func (f *fakeMobileLogs) Phones() ([]MobilePhone, error) { return f.phones, nil }

func (f *fakeMobileLogs) Read(_ context.Context, deviceID string, sinceMinutes int) (any, error) {
	f.reads = append(f.reads, deviceID)
	if f.fail != nil {
		return nil, f.fail
	}
	return map[string]any{"path": "/tmp/phone.jsonl", "device_id": deviceID, "since_minutes": sinceMinutes}, nil
}

func TestChoosePhone(t *testing.T) {
	a := MobilePhone{ID: "ios-a", PeerID: "a", Name: "Multica iOS", Connected: true}
	b := MobilePhone{ID: "ios-b", PeerID: "b", Name: "Multica iOS", Connected: true}
	offline := MobilePhone{ID: "ios-c", PeerID: "c", Name: "Multica iOS"}
	fromPhone := func(id string) TurnOrigin { return TurnOrigin{PeerID: id, ClientKind: timing.ClientMobile} }
	cases := []struct {
		name    string
		phones  []MobilePhone
		device  string
		origin  TurnOrigin
		want    string
		wantErr string
	}{
		{name: "one connected phone", phones: []MobilePhone{a, offline}, want: "ios-a"},
		{name: "one known offline phone is still tried", phones: []MobilePhone{offline}, want: "ios-c"},
		{name: "a named offline phone is tried", phones: []MobilePhone{a, offline}, device: "ios-c", want: "ios-c"},
		{name: "the phone the turn came from", phones: []MobilePhone{a, b}, origin: fromPhone("b"), want: "ios-b"},
		{name: "a named phone wins over the turn's", phones: []MobilePhone{a, b}, device: "ios-a", origin: fromPhone("b"), want: "ios-a"},
		{name: "the turn's phone offline is not swapped for another", phones: []MobilePhone{a, offline}, origin: fromPhone("c"), want: "ios-c"},
		{name: "the turn's phone gone is not swapped for another", phones: []MobilePhone{a}, origin: fromPhone("z"), wantErr: "no longer"},
		{name: "another computer's turn says nothing about phones", phones: []MobilePhone{a}, origin: TurnOrigin{PeerID: "mac", ClientKind: timing.ClientDesktop}, want: "ios-a"},
		{name: "two connected phones are never guessed", phones: []MobilePhone{a, b}, wantErr: "ios-b"},
		{name: "two offline phones are never guessed", phones: []MobilePhone{offline, {ID: "ios-d"}}, wantErr: "No phone is connected"},
		{name: "unknown id lists the choices", phones: []MobilePhone{a}, device: "ios-z", wantErr: "ios-a"},
		{name: "no phone", wantErr: "no phone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := choosePhone(tc.phones, tc.device, tc.origin)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got.ID != tc.want {
				t.Fatalf("got %q, %v; want %q", got.ID, err, tc.want)
			}
		})
	}
}

func noTurn() map[string]any { return map[string]any{} }

func TestFetchMobileLogsReadsOncePerRequestAndRetriesAFailure(t *testing.T) {
	t.Parallel()
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	logs := &fakeMobileLogs{phones: []MobilePhone{{ID: "ios-a", Connected: true}}, fail: errors.New("the app is not open")}
	tool := f.svc.mobileLogTool("conv-1", noTurn, logs)
	ctx := context.Background()
	if _, err := tool.Call(ctx, json.RawMessage(`{"request_id":"r1"}`)); err == nil {
		t.Fatal("a failed read reported success")
	}
	logs.fail = nil
	for range 2 {
		res, err := tool.Call(ctx, json.RawMessage(`{"request_id":"r1"}`))
		if err != nil || res.Value.(map[string]any)["since_minutes"] != 60 {
			t.Fatalf("read = %+v, %v", res, err)
		}
	}
	if len(logs.reads) != 2 {
		t.Fatalf("reads = %v, want the failure and one success", logs.reads)
	}
	if _, err := tool.Call(ctx, json.RawMessage(`{"request_id":"r1","since_minutes":5}`)); err == nil {
		t.Fatal("reused a request id for another read")
	}
}

func TestTurnsRecordTheDeviceThatAskedForThem(t *testing.T) {
	t.Parallel()
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	phone := TurnOrigin{PeerID: "phone-peer", ClientKind: timing.ClientMobile}
	other := TurnOrigin{PeerID: "other-phone", ClientKind: timing.ClientMobile}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r0", Text: "first", Origin: phone}); err != nil {
		t.Fatal(err)
	}
	origin := func(turnID string) TurnOrigin {
		return f.svc.currentOrigin(ctx, "conv-1", func() map[string]any { return map[string]any{"turn_id": turnID} })
	}
	if got := origin("turn-r0"); got != phone {
		t.Fatalf("sent turn origin = %+v", got)
	}
	// A queued message keeps who queued it, not whoever is connected when it runs.
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv-1", queued("q1", "second"), false, other); err != nil {
		t.Fatal(err)
	}
	state, err := f.svc.store.Queue(ctx, "conv-1")
	if err != nil || len(state.Items) != 1 || state.Items[0].OriginPeerID != "other-phone" || state.Items[0].OriginClientKind != timing.ClientMobile {
		t.Fatalf("queued = %+v, %v", state, err)
	}
	f.adapters[0].emit(t, turnDone)
	eventually(t, "the queued message", func() bool { return fmt.Sprint(f.sends(t)) == "[first second]" })
	if got := origin("turn-q1"); got != other {
		t.Fatalf("queued turn origin = %+v", got)
	}
	if got := origin("turn-missing"); got != (TurnOrigin{}) {
		t.Fatalf("unknown turn origin = %+v", got)
	}
}
