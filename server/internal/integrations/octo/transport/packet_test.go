package transport

import "testing"

// TestParseSettingByte covers the bit extraction for the RECV setting byte. A
// wrong shift/mask here misaligns every wire field that follows the topic flag,
// so each documented bit is asserted in isolation and in combination.
func TestParseSettingByte(t *testing.T) {
	cases := []struct {
		name         string
		v            byte
		wantTopic    bool
		wantStreamOn bool
	}{
		{"zero", 0x00, false, false},
		{"streamOn only (bit 1)", 0x02, false, true},
		{"topic only (bit 3)", 0x08, true, false},
		{"topic + streamOn", 0x0A, true, true},
		{"all bits set", 0xFF, true, true},
		{"unrelated bits ignored", 0x05, false, false}, // bits 0 and 2 set, not 1 or 3
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseSettingByte(c.v)
			if got.topic != c.wantTopic {
				t.Errorf("topic = %v, want %v (byte %#02x)", got.topic, c.wantTopic, c.v)
			}
			if got.streamOn != c.wantStreamOn {
				t.Errorf("streamOn = %v, want %v (byte %#02x)", got.streamOn, c.wantStreamOn, c.v)
			}
		})
	}
}
