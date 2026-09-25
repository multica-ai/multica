package wecom

// credentials.go — the CredentialsResolver default implementation that
// unseals an Installation's encrypted secret for the WebSocket subscribe
// frame. The wecom package owns encryption via a secretbox.Box supplied at
// boot; the Installation itself never holds plaintext, so every caller that
// needs a real secret comes through here.

import (
	"errors"
	"fmt"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// CredentialsResolver mints per-call InstallationCredentials with plaintext
// secrets by unsealing what the Installation carries. The wecom package
// injects a concrete implementation at boot; unit tests supply a fake.
type CredentialsResolver interface {
	Credentials(inst Installation) (InstallationCredentials, error)
}

// SecretboxCredentialsResolver decrypts the smart-bot secret using a single
// secretbox.Box shared across every wecom installation. Rotation is the same
// story as Feishu / Slack: change MULTICA_WECOM_SECRET_KEY, and every
// existing row needs a re-encrypt migration.
type SecretboxCredentialsResolver struct {
	Box *secretbox.Box
}

// NewSecretboxCredentialsResolver validates the box is non-nil so the wire-
// up cannot fall back to plaintext.
func NewSecretboxCredentialsResolver(box *secretbox.Box) (*SecretboxCredentialsResolver, error) {
	if box == nil {
		return nil, errors.New("wecom: SecretboxCredentialsResolver requires a non-nil secretbox.Box")
	}
	return &SecretboxCredentialsResolver{Box: box}, nil
}

// Credentials returns the plaintext-bearing InstallationCredentials for a
// given Installation. The returned value is a value type, safe to hand
// around without aliasing the encrypted blob.
func (r *SecretboxCredentialsResolver) Credentials(inst Installation) (InstallationCredentials, error) {
	if r.Box == nil {
		return InstallationCredentials{}, errors.New("wecom: credentials resolver has no secretbox")
	}
	secret, err := r.Box.Open(inst.SecretEncrypted)
	if err != nil {
		return InstallationCredentials{}, fmt.Errorf("wecom: decrypt secret: %w", err)
	}
	return InstallationCredentials{
		BotID:  inst.BotID,
		Secret: string(secret),
	}, nil
}
