# Bundled Plugin release catalog

This package is the production acquisition boundary for official Plugins that
ship with the Multica server. It discovers release directories generically,
rebuilds their ZIP archives deterministically, validates the manifest and
artifact, and verifies the detached Ed25519 signature against
`trust_roots.json` before exposing a release to the catalog.

Each release directory contains:

- `release.json`: source and publisher metadata plus the signing-key ID.
- `release.sig`: a detached base64 Ed25519 signature.
- `multica.plugin.json` and the static contribution payloads that form the ZIP.

The signing private key is intentionally not stored in this repository or the
server binary. Release automation must build the same deterministic ZIP, sign
the Plugin release envelope offline, and commit only the detached signature.
Run the reproducibility and trust-boundary checks from `server/`:

```sh
go test ./internal/pluginbundled -count=1
```

A malformed, tampered, or unknown-key release is isolated as a safe catalog
diagnostic. It does not prevent other releases or the server from starting.
The public API admits only this bundled source in V1; no `private_dev` upload or
server-file-path acquisition route is exposed.
