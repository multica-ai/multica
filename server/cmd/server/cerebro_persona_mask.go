// JEH-1079: persona mask wiring for the router.
//
// Reads the same MULTICA_PERSONA_URL / MULTICA_PERSONA_TOKEN env vars
// the existing persona handler already uses. When either is empty the
// returned invoker is nil — handler.maskDecide treats that as "no
// persona, no redaction" and the API behaves as before.
//
// Kept in a cerebro-prefixed file so the router's CEREBRO-PATCH on
// h.PersonaMask is a single line and the construction logic lives
// outside the upstream file.

package main

import (
	"log/slog"
	"os"
	"strings"

	persona "github.com/hvejsel/firtal-persona/sdk/go"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebropersonamask "github.com/multica-ai/multica/server/internal/cerebro/persona/mask"
	"github.com/multica-ai/multica/server/internal/handler"
)

// newPersonaMaskInvoker returns the bridge the upstream handler stores
// as h.PersonaMask, or nil when persona is not configured for this
// process.
func newPersonaMaskInvoker() handler.PersonaMaskInvoker {
	url := strings.TrimSpace(os.Getenv("MULTICA_PERSONA_URL"))
	token := strings.TrimSpace(os.Getenv("MULTICA_PERSONA_TOKEN"))
	if url == "" || token == "" {
		return nil
	}
	client, err := persona.New(persona.Config{Endpoint: url, Token: token})
	if err != nil {
		// Configuration was set but malformed — log and fall back to
		// no-redaction rather than crashing the server. The persona
		// integration is fail-closed only inside the request path; at
		// startup we prefer "no masking" over "no server".
		slog.Default().Warn("persona-mask: client init failed, masking disabled", "err", err)
		return nil
	}
	checker := cerebropersonamask.NewChecker(client, slog.Default())
	return cerebropersonamask.NewHandlerBridge(checker)
}

// newPersonaMaskAuditWriter wires the JEH-1173 redaction ledger writer.
// Independent of persona env: a deny / allow-with-mask decision still
// gets logged even if the upstream PersonaMask invoker is the no-op
// fallback (which happens only in dev/CI without persona configured —
// in that case PersonaMask returns Allowed=true with no MaskedFields, so
// recordPersonaMaskRedaction never fires anyway).
func newPersonaMaskAuditWriter(q *cerebrodb.Queries) handler.PersonaMaskAuditWriter {
	if q == nil {
		return nil
	}
	return cerebropersonamask.NewAuditWriter(q, slog.Default())
}
