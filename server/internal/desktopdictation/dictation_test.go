package desktopdictation

import (
	"fmt"
	"reflect"
	"testing"
)

type fakePlatform struct {
	ready       bool
	running     bool
	window      uintptr
	down        uint16
	sent        int
	calls       [][]keyEvent
	checks      int
	switchAt    int
	cleanupSent *int
}

func (f *fakePlatform) available() bool         { return f.ready }
func (f *fakePlatform) desktopRunning() bool    { return f.running }
func (f *fakePlatform) keyDown(key uint16) bool { return key == f.down }
func (f *fakePlatform) foregroundWindow() uintptr {
	f.checks++
	if f.switchAt != 0 && f.checks >= f.switchAt {
		return 99
	}
	return f.window
}
func (f *fakePlatform) sendKeys(keys []keyEvent) int {
	f.calls = append(f.calls, append([]keyEvent(nil), keys...))
	if len(f.calls) == 1 {
		return f.sent
	}
	if f.cleanupSent != nil {
		return *f.cleanupSent
	}
	return len(keys)
}

func healthyPlatform() *fakePlatform {
	return &fakePlatform{ready: true, running: true, window: 42, sent: 8}
}

func TestInvalidRequestsNeverReachNativeCode(t *testing.T) {
	for _, args := range [][]string{
		nil, {"toggle"}, {"probe", "42"}, {"record", "42"},
		{"toggle", "0"}, {"toggle", "-1"}, {"toggle", "+42"},
		{"toggle", " 42"}, {"toggle", "0x2a"}, {"toggle", "042"},
		{"toggle", "18446744073709551616"}, {"toggle", "42", "extra"},
	} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			if got := run(args, nil); got != "unavailable" {
				t.Fatalf("status = %q, want unavailable", got)
			}
		})
	}
}

func TestProbeNeverFocusesOrSendsInput(t *testing.T) {
	f := healthyPlatform()
	if got := run([]string{"probe"}, f); got != "ready" {
		t.Fatalf("status = %q, want ready", got)
	}
	if len(f.calls) != 0 || f.checks != 0 {
		t.Fatal("a probe must not inspect the target or send input")
	}
}

func TestNativeAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*fakePlatform)
		want   string
	}{
		{"unsupported", func(f *fakePlatform) { f.ready = false }, "unavailable"},
		{"no desktop", func(f *fakePlatform) { f.running = false }, "app_not_running"},
		{"wrong window", func(f *fakePlatform) { f.window = 99 }, "not_focused"},
		{"focus changed", func(f *fakePlatform) { f.switchAt = 2 }, "not_focused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := healthyPlatform()
			tc.mutate(f)
			if got := run([]string{"toggle", "42"}, f); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
			if len(f.calls) != 0 {
				t.Fatal("rejected requests must not send input")
			}
		})
	}
}

func TestHeldKeysPreventModifiedChordsOrAccidentalSubmit(t *testing.T) {
	for _, key := range guardedKeys {
		f := healthyPlatform()
		f.down = key
		if got := run([]string{"toggle", "42"}, f); got != "busy" || len(f.calls) != 0 {
			t.Fatalf("held key %#x: status = %q, sends = %d", key, got, len(f.calls))
		}
	}
}

func TestToggleSendsOnlyTheFixedChordInOneBatch(t *testing.T) {
	f := healthyPlatform()
	if got := run([]string{"toggle", "42"}, f); got != "sent" {
		t.Fatalf("status = %q, want sent", got)
	}
	want := [][]keyEvent{{
		{key: 0x11}, {key: 0x12}, {key: 0x10}, {key: 0x44},
		{key: 0x44, up: true}, {key: 0x10, up: true}, {key: 0x12, up: true}, {key: 0x11, up: true},
	}}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("input = %#v, want %#v", f.calls, want)
	}
}

func TestPartialInjectionReleasesOnlyOutstandingKeysWithoutRetry(t *testing.T) {
	for sent := 0; sent < 8; sent++ {
		t.Run(fmt.Sprint(sent), func(t *testing.T) {
			f := healthyPlatform()
			f.sent = sent
			if got := run([]string{"toggle", "42"}, f); got != "unavailable" {
				t.Fatalf("status = %q, want unavailable", got)
			}
			if sent == 0 {
				if len(f.calls) != 1 {
					t.Fatal("no keys were inserted, so no release should be sent")
				}
				return
			}
			if len(f.calls) != 2 {
				t.Fatalf("got %d calls; want chord and one cleanup batch", len(f.calls))
			}
			held := map[uint16]bool{}
			for _, key := range f.calls[0][:sent] {
				held[key.key] = !key.up
			}
			for _, key := range f.calls[1] {
				if !key.up || !held[key.key] {
					t.Fatalf("unexpected cleanup key %#v", key)
				}
				held[key.key] = false
			}
			for key, down := range held {
				if down {
					t.Fatalf("key %#x left down", key)
				}
			}
		})
	}
}

func TestFailedCleanupReportsHeldKeysWithoutAnotherToggle(t *testing.T) {
	f := healthyPlatform()
	f.sent = 4
	count := 2
	f.cleanupSent = &count
	if got := run([]string{"toggle", "42"}, f); got != "cleanup_failed" {
		t.Fatalf("status = %q, want cleanup_failed", got)
	}
	if len(f.calls) != 2 {
		t.Fatalf("got %d calls; want one chord and one release attempt", len(f.calls))
	}
}
