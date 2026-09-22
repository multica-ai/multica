package handler

import (
	"net/http/httptest"
	"testing"
)

func TestKnowledgePrivateAccessAllowed(t *testing.T) {
	cases := []struct {
		name        string
		actorSource string
		want        bool
	}{
		{name: "human", actorSource: "", want: true},
		{name: "task token", actorSource: "task_token", want: false},
		{name: "cloud pat", actorSource: "cloud_pat", want: false},
		{name: "unknown source remains human equivalent", actorSource: "future_actor", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := knowledgePrivateAccessAllowed(tc.actorSource); got != tc.want {
				t.Fatalf("knowledgePrivateAccessAllowed(%q) = %v, want %v", tc.actorSource, got, tc.want)
			}
		})
	}
}

func TestSetKnowledgeContentHeadersForcesDownload(t *testing.T) {
	recorder := httptest.NewRecorder()
	setKnowledgeContentHeaders(recorder, "report.html")

	if got := recorder.Header().Get("Content-Disposition"); got != `attachment; filename="report.html"` {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
}
