package handler

import (
	"context"
	"errors"

	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var errGiteaIdentityAlreadyLinked = errors.New("Gitea identity is already linked to another Multica user")

func (h *Handler) findOrCreateGiteaUser(ctx context.Context, issuer, subject, email string) (db.User, bool, error) {
	identity, err := h.Queries.GetExternalAuthIdentity(ctx, db.GetExternalAuthIdentityParams{
		Provider: "gitea",
		Issuer:   issuer,
		Subject:  subject,
	})
	if err == nil {
		user, userErr := h.Queries.GetUser(ctx, identity.UserID)
		if userErr != nil {
			return db.User{}, false, userErr
		}
		if auth.IsTemporarilyDisabledUser(uuidToString(user.ID), user.Email) {
			return db.User{}, false, auth.ErrTemporarilyDisabledUser
		}
		return user, false, nil
	}
	if !isNotFound(err) {
		return db.User{}, false, err
	}

	user, isNew, err := h.findOrCreateUser(ctx, email)
	if err != nil {
		return db.User{}, false, err
	}
	linked, err := h.Queries.CreateExternalAuthIdentity(ctx, db.CreateExternalAuthIdentityParams{
		UserID:   user.ID,
		Provider: "gitea",
		Issuer:   issuer,
		Subject:  subject,
	})
	if err != nil {
		return db.User{}, false, err
	}
	if linked.UserID != user.ID {
		return db.User{}, false, errGiteaIdentityAlreadyLinked
	}
	return user, isNew, nil
}
