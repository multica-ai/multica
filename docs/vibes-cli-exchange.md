# VIBES CLI exchange v1

Multica consumes VIBES' dedicated `vibes-multica-cli-v1` one-time authorization
and then owns the existing CLI PAT, profile, daemon, and Runtime credential
journey. This does not create a Multica browser identity system.

Public Go contract:

- `vibeshandoff.CLIAudience`
- `vibeshandoff.CLISchemaVersion`
- `vibeshandoff.CLIConsumeRequest`
- `vibeshandoff.CLIIdentity`
- `vibeshandoff.NewCLIClient`
- `tagaccess.CLITagSessionID`

The CLI generates state, a PKCE verifier/challenge, and receiver identity; opens
the VIBES `/api/tag-cli/authorize` URL; and uses only a localhost, loopback, or
RFC1918 private-IPv4 `/callback` listener. VIBES shows the exact Workspace and
receiver and requires an explicit same-origin approval; the authorize GET never
issues a code. The CLI accepts only opaque `code + state` at the exact callback
and exchanges code, verifier, and receiver binding at
`POST /api/auth/vibes-cli-exchange`. Remote authorize and exchange endpoints
must use HTTPS. Loopback HTTP remains available for disposable local tests.

The server consumes VIBES authority, grants and immediately checks the #289
Gate, mirrors only stable IDs, and atomically creates the ordinary hash-only PAT
plus `vibes_cli_pat_binding`. Every regular or daemon use of that PAT bypasses
the native PAT cache, rechecks the exact VIBES session/account/Workspace/member
binding through the Gate, and pins request Workspace resolution to the durable
binding. Ban, logout, membership removal, Workspace supersession, expiry, or an
authority-store outage therefore fails closed after issuance too.

Configuration is an all-or-nothing pair:

- `VIBES_CLI_CONSUME_URL`
- `VIBES_CLI_EXCHANGE_SERVICE_SECRET` (minimum 32 characters)

Both empty explicitly disable exchange. Partial, weak, or remote HTTP
configuration stops server startup. The deterministic Go-compatible fixture is
`server/internal/vibeshandoff/testdata/vibes-cli-exchange-v1.json`.

Legacy browser `cli_callback` and raw JWT CLI callback surfaces are removed.
The existing `/api/cli-token` route is retained only for the Desktop-owned
deep-link handoff deferred to #296; neither this CLI nor the new exchange calls
it. Neither code, verifier, PAT, JWT, cookie, nor service secret belongs in the
new CLI browser URL, callback, log, or audit record.
