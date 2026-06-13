package wecom

import (
	"testing"
	"time"
)

func TestBindingTokenTTLBelowDBCheckCap(t *testing.T) {
	const dbCap = 15 * time.Minute
	if BindingTokenTTL >= dbCap {
		t.Fatalf("BindingTokenTTL=%s must stay below DB cap %s", BindingTokenTTL, dbCap)
	}
}
