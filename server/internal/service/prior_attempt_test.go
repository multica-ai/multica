package service

import (
	"strings"
	"testing"
)

func TestPriorAttemptErrorTail(t *testing.T) {
	if got := priorAttemptErrorTail("short"); got != "short" {
		t.Errorf("short error must pass through, got %q", got)
	}

	long := strings.Repeat("x", priorAttemptErrorTailMax+500)
	got := priorAttemptErrorTail(long)
	wantSuffix := long[len(long)-priorAttemptErrorTailMax:]
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("tail must keep final %d bytes", priorAttemptErrorTailMax)
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("tail must be marked with leading ellipsis")
	}
	if len(got) > priorAttemptErrorTailMax+3 { // ellipsis = 3 bytes in UTF-8
		t.Errorf("tail too long: %d bytes", len(got))
	}
}
