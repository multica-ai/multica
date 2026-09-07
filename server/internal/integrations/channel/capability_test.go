package channel

import "testing"

func TestCapability_Has(t *testing.T) {
	c := CapText | CapThreadReply

	if !c.Has(CapText) {
		t.Errorf("Has(CapText) = false, want true")
	}
	if !c.Has(CapText | CapThreadReply) {
		t.Errorf("Has(text|thread) = false, want true (includes-all)")
	}
	if c.Has(CapRichCard) {
		t.Errorf("Has(CapRichCard) = true, want false")
	}
	if c.Has(CapText | CapRichCard) {
		t.Errorf("Has(text|rich) = true, want false (one bit missing)")
	}
	// The empty requirement is always satisfied.
	if !c.Has(0) {
		t.Errorf("Has(0) = false, want true")
	}
}

func TestCapability_String(t *testing.T) {
	tests := []struct {
		name string
		cap  Capability
		want string
	}{
		{"zero", 0, "none"},
		{"single", CapText, "text"},
		{"ordered_lsb_first", CapThreadReply | CapText, "text|thread_reply"},
		{"all_named", CapMessageEdit, "message_edit"},
		{"unknown_high_bit", CapText | (1 << 40), "text|0x10000000000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cap.String(); got != tt.want {
				t.Errorf("Capability(%#x).String() = %q, want %q", uint64(tt.cap), got, tt.want)
			}
		})
	}
}
