# Agent templates

Curated agent templates served by the "create agent from template" flow. Each
template is a single `<slug>.json` file (schema: `../types.go`), embedded at
build time by `../loader.go` and loaded into an in-memory registry at server
startup. The filename must equal the template's `slug`.

This directory is intentionally empty of templates right now. Add one by
dropping a `<slug>.json` file here. This README is a placeholder so that
`//go:embed templates` still compiles while the catalog is empty (a
`templates/*.json` glob with zero matches is a build error). `loadFromFS` only
reads `*.json`, so this file is ignored at load time.
