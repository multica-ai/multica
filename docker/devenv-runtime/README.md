# devenv-runtime

> **NOTE — staging.** Per [PLA-267] this directory was scoped to land in
> `g2crowd/docker-layers/DevOps/devenv-runtime/`. It lives in `agentfarm`
> alongside `docker/agent-runtime-base/` because the base image now builds
> the multica binary out of this repo's `server/cmd/multica`. When the base
> moves to unforked `multica-ai/multica` (and its directory can lift to
> `docker-layers`), this directory lifts with it.

Layers on top of `ghcr.io/g2crowd/agent-runtime-base` to run the
`opencode serve` HTTP server. Replaces the install-everything `opencode-dev`
image (cutover deferred per PLA-267 — keep both for a release until this
one is proven in production).

## Image

`ghcr.io/g2crowd/devenv-runtime:<tag>` — multi-arch (`linux/amd64` +
`linux/arm64`). Inherits every tool from the base; the only thing layered
on is the entrypoint that boots `opencode serve`.

## Runtime env

| Var                 | Default     | Notes                                                                    |
|---------------------|-------------|--------------------------------------------------------------------------|
| `GH_TOKEN`          | (empty)     | GitHub PAT. When present, the entrypoint runs `gh auth setup-git --hostname github.com` so plain `git clone https://github.com/<org>/<repo>` against private repos works without prompting. SSH-style remotes (`git@github.com:…`, `ssh://git@github.com/…`) are also rewritten to HTTPS so they pick up the same credential helper. The token is never written to disk — `gh` is wired as the credential helper and reads `GH_TOKEN` from the process env on demand. |
| `OPENCODE_HOST`     | `0.0.0.0`   | Bind address. Defaults to `0.0.0.0` (overriding upstream's `127.0.0.1`) so the Kubernetes Service can reach the pod. |
| `OPENCODE_PORT`     | `4096`      | Matches the upstream `opencode serve` default.                           |
| `OPENCODE_EXTRA_ARGS` | (empty)    | Appended verbatim, e.g. `--cors https://devenv-jshuff.development.g2.com`. |
| `KUBECTL_MINOR`       | `v1.31`    | Selects the upstream Kubernetes apt repo. Keep aligned with EKS control-plane minor. |

## Smoke test

```bash
# Build locally, then verify the HTTP API answers
docker run --rm -d --name devenv-runtime-smoke \
  -p 4096:4096 \
  ghcr.io/g2crowd/devenv-runtime:<tag>

# Hit the OpenAPI endpoint
curl -fsS http://localhost:4096/doc | jq '.info'

docker rm -f devenv-runtime-smoke
```

The `/doc` endpoint is exposed by `opencode serve` per
[opencode.ai/docs/server](https://opencode.ai/docs/server) and is the
simplest readiness signal.
