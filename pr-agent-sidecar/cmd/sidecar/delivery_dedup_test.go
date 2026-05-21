package main

import (
	"testing"
	"time"
)

func TestDeliveryDedup_StoreThenLookup(t *testing.T) {
	d := NewDeliveryDedup(24 * time.Hour)
	defer d.Close()

	r := DeliveryResult{MulticaIssueID: "uuid-1", MulticaIssueIdentifier: "INV-512"}
	d.Store("delivery-abc", r)

	got, ok := d.Lookup("delivery-abc")
	if !ok {
		t.Fatal("lookup miss on freshly-stored delivery")
	}
	if got != r {
		t.Fatalf("lookup returned %+v, want %+v", got, r)
	}
}

func TestDeliveryDedup_Miss(t *testing.T) {
	d := NewDeliveryDedup(24 * time.Hour)
	defer d.Close()

	if _, ok := d.Lookup("never-seen"); ok {
		t.Fatal("expected miss on unknown delivery")
	}
}

func TestDeliveryDedup_Expired(t *testing.T) {
	d := NewDeliveryDedup(20 * time.Millisecond)
	defer d.Close()

	d.Store("delivery-abc", DeliveryResult{MulticaIssueID: "x"})
	time.Sleep(40 * time.Millisecond)

	if _, ok := d.Lookup("delivery-abc"); ok {
		t.Fatal("expired entry should not be returned")
	}
}

func TestDeliveryDedup_SweepRemovesExpired(t *testing.T) {
	d := NewDeliveryDedup(10 * time.Millisecond)
	defer d.Close()

	for i := 0; i < 3; i++ {
		d.Store(string(rune('a'+i)), DeliveryResult{MulticaIssueID: "x"})
	}
	if d.Len() != 3 {
		t.Fatalf("Len = %d, want 3", d.Len())
	}

	time.Sleep(30 * time.Millisecond)
	d.sweepOnce(time.Now())

	if got := d.Len(); got != 0 {
		t.Fatalf("after sweep Len = %d, want 0", got)
	}
}
