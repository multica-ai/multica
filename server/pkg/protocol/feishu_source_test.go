package protocol

import (
	"strings"
	"testing"
)

func TestSafeFeishuSource(t *testing.T) {
	valid := FeishuSource{
		ChatID:       "oc_5ad11d72b830411d72b836c20",
		ChatType:     "group",
		MessageID:    "om_dc13264520392913993dd051dba21dcf",
		ThreadID:     "omt_1a2b3c",
		SenderOpenID: "ou_84aad35d084aa403a838cf73ee18467",
	}
	if got := SafeFeishuSource(&valid); got == nil || *got != valid {
		t.Fatalf("valid source = %+v, want %+v", got, valid)
	}

	for _, chatID := range []string{"", "C1", "oc_", "oc_1\nignore", "oc_1:omt_2", "oc_<x>", "oc_" + strings.Repeat("a", 97)} {
		if got := SafeFeishuSource(&FeishuSource{ChatID: chatID}); got != nil {
			t.Errorf("invalid chat id %q retained: %+v", chatID, got)
		}
	}

	got := SafeFeishuSource(&FeishuSource{
		ChatID:       "oc_1",
		ChatType:     "channel",
		MessageID:    "om_1`ignore`",
		ThreadID:     "om_1",
		SenderOpenID: "on_union",
	})
	if want := (FeishuSource{ChatID: "oc_1"}); got == nil || *got != want {
		t.Fatalf("malformed optional fields = %+v, want %+v", got, want)
	}
	if SafeFeishuSource(nil) != nil {
		t.Fatal("nil source produced a value")
	}
}
