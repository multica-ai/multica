package qianwen

import (
	"encoding/json"
	"testing"
)

func TestDecodePublicConfigMalformedJSONReturnsZeroValue(t *testing.T) {
	got := DecodePublicConfig(json.RawMessage(`{"app_id":"qwc_partial","mode":123}`))
	if got != (PublicConfig{}) {
		t.Fatalf("DecodePublicConfig() = %+v, want zero value", got)
	}
}
