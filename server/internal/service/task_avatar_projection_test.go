package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAgentToMapAvatarProjection(t *testing.T) {
	seed := "44444444-4444-4444-4444-444444444444"
	tests := []struct {
		name   string
		stored pgtype.Text
		want   string
	}{
		{name: "uploaded image", stored: pgtype.Text{String: "https://profiles.example.com/a.png", Valid: true}, want: "https://profiles.example.com/a.png"},
		{name: "existing emoji", stored: pgtype.Text{String: "emoji:🦉", Valid: true}, want: "emoji:🦉"},
		{name: "generated fallback", want: "gen:" + seed},
		{name: "invalid emoji falls through", stored: pgtype.Text{String: "emoji:   ", Valid: true}, want: "gen:" + seed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := agentToMap(db.Agent{AvatarUrl: tt.stored, AvatarSeed: seed})
			got, ok := payload["avatar_url"].(*string)
			if !ok || got == nil || *got != tt.want {
				t.Fatalf("agentToMap avatar_url = %#v, want %q", payload["avatar_url"], tt.want)
			}
		})
	}
}
