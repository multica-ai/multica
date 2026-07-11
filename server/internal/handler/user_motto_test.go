package handler

import (
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestUserToResponseIncludesMotto(t *testing.T) {
	resp := userToResponse(db.User{Motto: "Ship small, learn fast."})

	if resp.Motto != "Ship small, learn fast." {
		t.Fatalf("expected motto to round trip, got %q", resp.Motto)
	}
}

func TestUpdateMeRequestAcceptsMotto(t *testing.T) {
	req := UpdateMeRequest{Motto: ptr("Ship small, learn fast.")}

	if req.Motto == nil || *req.Motto != "Ship small, learn fast." {
		t.Fatalf("expected motto request field to decode, got %#v", req.Motto)
	}
}
