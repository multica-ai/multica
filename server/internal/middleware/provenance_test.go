package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestProvenanceFromRequest(t *testing.T) {
	userID := uuid.New()
	taskID := uuid.New()

	tests := []struct {
		name           string
		authPath       string
		userIDHeader   string
		taskIDHeader   string
		curatorHeader  string
		wantType       string
		wantAuthorNil  bool
		wantTaskNil    bool
	}{
		{
			name:          "daemon token without curator header",
			authPath:      DaemonAuthPathDaemonToken,
			taskIDHeader:  taskID.String(),
			wantType:      "agent_foreground",
			wantAuthorNil: true,
			wantTaskNil:   false,
		},
		{
			name:          "daemon token with curator header",
			authPath:      DaemonAuthPathDaemonToken,
			curatorHeader: "true",
			wantType:      "agent_background",
			wantAuthorNil: true,
			wantTaskNil:   true,
		},
		{
			name:          "daemon token alone (no task ID)",
			authPath:      DaemonAuthPathDaemonToken,
			wantType:      "agent_foreground",
			wantAuthorNil: true,
			wantTaskNil:   true,
		},
		{
			// Daemon-token clients can SET X-User-ID on outgoing requests
			// (the daemon-token branch of DaemonAuth does not overwrite it
			// the way the PAT/JWT branches do). ProvenanceFromRequest must
			// ignore that value to prevent author/audit forgery — recording
			// the spoofed UUID as the AuthorID would let a daemon token
			// attribute an agent-driven edit to any user it names.
			name:          "daemon token IGNORES client-supplied X-User-ID",
			authPath:      DaemonAuthPathDaemonToken,
			userIDHeader:  userID.String(),
			wantType:      "agent_foreground",
			wantAuthorNil: true,
			wantTaskNil:   true,
		},
		{
			name:          "PAT token",
			authPath:      DaemonAuthPathPAT,
			userIDHeader:  userID.String(),
			wantType:      "human",
			wantAuthorNil: false,
			wantTaskNil:   true,
		},
		{
			name:          "JWT token",
			authPath:      DaemonAuthPathJWT,
			userIDHeader:  userID.String(),
			wantType:      "human",
			wantAuthorNil: false,
			wantTaskNil:   true,
		},
		{
			name:          "no auth path (defaults to human)",
			authPath:      "",
			userIDHeader:  userID.String(),
			wantType:      "human",
			wantAuthorNil: false,
			wantTaskNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)

			if tt.authPath != "" {
				ctx := context.WithValue(r.Context(), ctxKeyDaemonAuthPath, tt.authPath)
				r = r.WithContext(ctx)
			}
			if tt.userIDHeader != "" {
				r.Header.Set("X-User-ID", tt.userIDHeader)
			}
			if tt.taskIDHeader != "" {
				r.Header.Set("X-Multica-Task-ID", tt.taskIDHeader)
			}
			if tt.curatorHeader != "" {
				r.Header.Set("X-Curator-Run", tt.curatorHeader)
			}

			p := ProvenanceFromRequest(r)

			if p.AuthorType != tt.wantType {
				t.Errorf("AuthorType = %q, want %q", p.AuthorType, tt.wantType)
			}
			if (p.AuthorID == nil) != tt.wantAuthorNil {
				t.Errorf("AuthorID nil = %v, want nil = %v", p.AuthorID == nil, tt.wantAuthorNil)
			}
			if (p.TaskID == nil) != tt.wantTaskNil {
				t.Errorf("TaskID nil = %v, want nil = %v", p.TaskID == nil, tt.wantTaskNil)
			}
			if !tt.wantAuthorNil && p.AuthorID != nil && *p.AuthorID != userID {
				t.Errorf("AuthorID = %v, want %v", *p.AuthorID, userID)
			}
			if !tt.wantTaskNil && p.TaskID != nil && *p.TaskID != taskID {
				t.Errorf("TaskID = %v, want %v", *p.TaskID, taskID)
			}
		})
	}
}
