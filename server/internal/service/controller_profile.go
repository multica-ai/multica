package service

import (
	"context"
	"github.com/multica-ai/multica/server/internal/runcontrol"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Product instructions and embedded skill contents are execution inputs too.
// Bind them to enrollment so a server upgrade cannot silently change a run.
func LoadControllerProfile(ctx context.Context, q *db.Queries, a db.Agent) (runcontrol.ProfileSnapshot, error) {
	s, err := runcontrol.LoadProfileSnapshot(ctx, q, a)
	if err != nil {
		return s, err
	}
	s.BuiltinHash = runcontrol.Digest([]any{loadBuiltinSkills(a.SystemKey.String), legacyRedirectSkills(), ComposeMikaInstructions(a.Name, a.Instructions)})
	return s, nil
}

func ControllerProfileHash(ctx context.Context, q *db.Queries, a db.Agent) (string, error) {
	s, err := LoadControllerProfile(ctx, q, a)
	if err != nil {
		return "", err
	}
	return s.Hash(), nil
}
