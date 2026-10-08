package handler

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectAvatarDisplayPrecedenceAndImageResolution(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withAvatarStorage(t, &mockStorageNoCdn{}, "")

	seed := "11111111-1111-1111-1111-111111111111"
	storedImage := "https://cdn.example.com/" + testAvatarKey
	signedImage := avatarURLPathPrefix + signAvatarKey(testAvatarKey) + "/" + testAvatarKey
	tests := []struct {
		name   string
		stored pgtype.Text
		want   *string
	}{
		{name: "uploaded image wins and remains signed", stored: pgtype.Text{String: storedImage, Valid: true}, want: ptr(signedImage)},
		{name: "existing emoji wins", stored: pgtype.Text{String: "emoji:🦁", Valid: true}, want: ptr("emoji:🦁")},
		{name: "generated fallback", want: ptr("gen:" + seed)},
		{name: "empty emoji marker falls through", stored: pgtype.Text{String: "emoji:", Valid: true}, want: ptr("gen:" + seed)},
		{name: "whitespace emoji marker falls through", stored: pgtype.Text{String: "emoji:   ", Valid: true}, want: ptr("gen:" + seed)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := testHandler.projectAvatarDisplay(tt.stored, seed)
			if got == nil || tt.want == nil {
				if got != tt.want {
					t.Fatalf("projectAvatarDisplay() = %v, want %v", got, tt.want)
				}
				return
			}
			if *got != *tt.want {
				t.Fatalf("projectAvatarDisplay() = %q, want %q", *got, *tt.want)
			}
		})
	}
}

func TestAgentResponseAvatarProjection(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	seed := "22222222-2222-2222-2222-222222222222"
	for _, tt := range []struct {
		name   string
		stored pgtype.Text
		want   string
	}{
		{name: "image", stored: pgtype.Text{String: "https://profiles.example.com/a.png", Valid: true}, want: "https://profiles.example.com/a.png"},
		{name: "emoji", stored: pgtype.Text{String: "emoji:🦇", Valid: true}, want: "emoji:🦇"},
		{name: "generated", want: "gen:" + seed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := testHandler.agentToResponse(db.Agent{AvatarUrl: tt.stored, AvatarSeed: seed}).AvatarURL
			if got == nil || *got != tt.want {
				t.Fatalf("agentToResponse avatar_url = %v, want %q", got, tt.want)
			}
		})
	}
}

func TestCommentAgentTriggerAvatarProjection(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	seed := "33333333-3333-3333-3333-333333333333"
	for _, tt := range []struct {
		name   string
		stored pgtype.Text
		want   string
	}{
		{name: "emoji", stored: pgtype.Text{String: "emoji:👔", Valid: true}, want: "emoji:👔"},
		{name: "generated", want: "gen:" + seed},
		{name: "invalid emoji falls through", stored: pgtype.Text{String: "emoji: ", Valid: true}, want: "gen:" + seed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := testHandler.commentAgentTriggerToResponse(commentAgentTrigger{
				Agent:  db.Agent{Name: "Avatar Agent", AvatarUrl: tt.stored, AvatarSeed: seed},
				Source: commentTriggerSourceMentionAgent,
			})
			if got.AvatarURL == nil || *got.AvatarURL != tt.want {
				t.Fatalf("comment trigger avatar_url = %v, want %q", got.AvatarURL, tt.want)
			}
		})
	}
}
