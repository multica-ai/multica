package service

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestContentFilterIsNotAutoRetried(t *testing.T) {
	reason := taskfailure.Classify("stream disconnected before completion: Incomplete response returned, reason: content_filter").String()
	if retryableReasons[reason] {
		t.Fatalf("content filtering was treated as retryable: %s", reason)
	}
}
