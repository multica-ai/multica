# agent-runtime-base

> **NOTE — staging.** Per [PLA-267] this directory was scoped to land in
> `g2crowd/docker-layers/DevOps/agent-runtime-base/`. It lives in `agentfarm`
> because the multica binary baked into the base is built from this repo's
> `server/cmd/multica` (the agentfarm fork — Jack's call on PLA-267). When we
> eventually move to unforked `multica-ai/multica`, the multica builder stage
> flips to a `git clone` and this directory can lift to `docker-layers`. Until
> then, the base lives where its multica source lives.

Shared base image for G2's AI-coding pipelines. DevEnv per-developer cloud
IDE pods install the toolchain from this image so it doesn't need to be
reinstalled independently.

Downstream images layer just an entrypoint on top — see
`docker/devenv-runtime/Dockerfile`.

## What's in it

Image: `ghcr.io/g2crowd/agent-runtime-base:<tag>` — multi-arch
(`linux/amd64` + `linux/arm64`).

| Tool      | Distribution                                    | Version pinned via       |
|-----------|-------------------------------------------------|--------------------------|
| `multica` | Built from `server/cmd/multica` (this repo)     | repo commit at bake time |
| `claude`  | npm `@anthropic-ai/claude-code`                 | `CLAUDE_CODE_VERSION`    |
| `codex`   | npm `@openai/codex`                             | `CODEX_VERSION`          |
| `opencode`| npm `opencode-ai`                               | `OPENCODE_VERSION`       |
| `pi`      | npm `@earendil-works/pi-coding-agent`           | `PI_VERSION`             |
| `hermes`  | [`NousResearch/hermes-agent`][hermes] installer | `HERMES_REF` (upstream release tag) + `HERMES_COMMIT` (verified SHA) |
| `gh`      | apt from `cli.github.com/packages`              | `GH_CLI_VERSION` (apt)   |

Common substrate: Debian `bookworm-slim`, Node `22` LTS, Go `1.26.1` (build
stage only), `git`, `bash`, `curl`, `jq`, `rsync`, `ripgrep`, `tini`,
`ca-certificates`. Non-root user `agent` at UID/GID `1000:1000`
(overridable via `UID`/`GID` build args).

`build-essential` is installed transiently during the hermes step (uv may
build Python wheels from source on aarch64 if no manylinux wheel exists) and
purged before the layer commits — it does not ship in the final image.

[hermes]:  https://github.com/NousResearch/hermes-agent

## Build args

All version-pinned via build args. Defaults match the Dockerfile so a bare
`docker buildx bake` works locally; CI passes explicit values per release.

| Build arg              | Default                                  | Notes                                                              |
|------------------------|------------------------------------------|--------------------------------------------------------------------|
| `NODE_VERSION`         | `22`                                     | Node LTS major. Bump in lockstep with hermes' supported Node range.|
| `GO_VERSION`           | `1.26.1`                                 | Matches CI (`CLAUDE.md`: "CI runs on Node 22 and Go 1.26.1").       |
| `MULTICA_VERSION`      | `dev`                                    | `-X main.version=` ldflag. CI passes the IMG_SHA or release tag.    |
| `MULTICA_COMMIT`       | `unknown`                                | `-X main.commit=` ldflag. CI passes the agentfarm commit SHA.       |
| `CLAUDE_CODE_VERSION`  | `latest`                                 | npm tag or version. Pin for prod (e.g. `1.7.4`).                    |
| `CODEX_VERSION`        | `latest`                                 | npm tag or version.                                                 |
| `OPENCODE_VERSION`     | `latest`                                 | npm tag or version.                                                 |
| `PI_VERSION`           | `latest`                                 | npm tag or version.                                                 |
| `HERMES_REF`           | `v2026.5.7`                              | Upstream calver release tag (`vYYYY.M.D`) in `NousResearch/hermes-agent`. The installer's `git clone --branch <ref>` accepts tags. Never default this back to a branch name like `main`. Bump together with `HERMES_COMMIT`. |
| `HERMES_COMMIT`        | `498bfc7bc12a937621b4215312049b1000726df3` | The commit SHA that `HERMES_REF` resolves to (peeled — what `git rev-parse HEAD` returns after `git clone --branch <tag>`, not the tag-object SHA). The Dockerfile verifies the installed HEAD matches; the build fails loudly if the tag has been force-moved. Resolve via `git ls-remote --tags https://github.com/NousResearch/hermes-agent <tag>^{}` (the `^{}` form gives you the peeled commit). |
| `GH_CLI_VERSION`       | (empty)                                  | apt version pin for `gh` (e.g. `2.63.0`). Empty = whatever cli.github.com ships at build time. |
| `UID`                  | `1000`                                   | Runtime user uid.                                                   |
| `GID`                  | `1000`                                   | Runtime user gid.                                                   |

## Smoke test

The Dockerfile runs the smoke test at build time and fails the build if any
tool refuses to start:

```bash
claude  --version
codex   --version
opencode --version
pi      --version
multica --version
gh      --version
hermes  --help     # hermes uses python-fire; --version is not surfaced
```

`hermes --version` is **not** in the smoke set on purpose — the project
distributes via git only and the python-fire entry point in
`hermes/cli.py` doesn't bind a `--version` flag. The version baked in is
whatever `HERMES_REF` points at; check `pip show hermes-agent` inside the
container if you need the resolved package version.

To run the smoke test manually against a built image:

```bash
docker run --rm ghcr.io/g2crowd/agent-runtime-base:<tag> \
  sh -c "claude --version && codex --version && opencode --version \
      && pi --version && multica --version && gh --version \
      && hermes --help >/dev/null"
```

## Building locally

Bake is invoked **from the agentfarm repo root** (the build context COPYs
from `server/`, so context = repo root):

```bash
# Single-arch local build for smoke testing
docker buildx bake -f docker/agent-runtime-base/docker-bake.hcl \
  --set default.platform=linux/$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') \
  --load default

# Multi-arch publish to ghcr (matches what CI does)
IMG_SHA=v0.1.0 \
MULTICA_VERSION=v0.1.0 \
MULTICA_COMMIT=$(git rev-parse --short HEAD) \
  docker buildx bake -f docker/agent-runtime-base/docker-bake.hcl --push default
```

## Bump procedure

The image is reproducible only as good as the args at bake time. When a
downstream consumer (devenv-runtime) needs a new version of
one of the baked tools:

1. **Identify which arg to bump.** Find the tool in the table above; the
   right column names the build arg.
2. **For `multica`:** no bump needed — every base bake picks up the
   agentfarm commit at HEAD via the build context, and CI passes the SHA
   through as `MULTICA_COMMIT`. Sync upstream into agentfarm first if you
   need a new multica daemon revision.
3. **For npm tools (`claude`, `codex`, `opencode`, `pi`):** bump the
   variable default in `docker-bake.hcl`. Commit.
4. **For `hermes`:** bump both `HERMES_REF` and `HERMES_COMMIT` in
   `docker-bake.hcl` together — they are a pair:
   1. Pick a release tag from
      [`NousResearch/hermes-agent/releases`](https://github.com/NousResearch/hermes-agent/releases)
      (use a `vYYYY.M.D` calver tag — never a branch name like `main`).
   2. Resolve the commit SHA that tag points at *after* annotated-tag
      peeling. The `^{}` form returns the commit, not the tag object:
      ```bash
      git ls-remote https://github.com/NousResearch/hermes-agent 'refs/tags/<tag>^{}'
      # prints: <commit-sha>  refs/tags/<tag>^{}
      ```
      (If a tag is *lightweight*, `^{}` and the bare tag form return the
      same SHA. Either way, use the SHA from the `^{}` form — that's what
      `git rev-parse HEAD` will be after `git clone --branch <tag>`.)
   3. Update both `HERMES_REF` and `HERMES_COMMIT` defaults in
      `docker-bake.hcl`. Commit them together.
   4. Smoke-test locally:
      ```bash
      docker buildx bake -f docker/agent-runtime-base/docker-bake.hcl \
        --set default.platform=linux/arm64 \
        --set default.output=type=docker \
        --set default.tags=test-hermes-pin:local default
      ```
      The Dockerfile's `rev-parse HEAD` check enforces alignment at build
      time — if the values disagree, the build fails with
      `HERMES_COMMIT mismatch: expected <X>, got <Y> (tag <REF> may have
      been moved)`. Same check fires in CI if upstream ever force-moves
      a published tag.
5. **For `gh` (GitHub CLI):** bump `GH_CLI_VERSION` (e.g. `2.63.0`) — pinned
   apt versions from `cli.github.com/packages`. Leave empty to take whatever
   ships at build time (acceptable for most rebuilds; pin if a specific gh
   release fixes a bug you care about).
6. **Open the PR.** CI bakes the new image on merge (see `publish.yml`'s
   `bake-build-agent-runtime-base` job) and publishes both
   `ghcr.io/g2crowd/agent-runtime-base:<sha>` and
   `ghcr.io/g2crowd/agent-runtime-base:latest`.
7. **Bump consumers** in a separate PR. Each downstream image FROMs
   `agent-runtime-base:<tag>` — bump `BASE_TAG` in their bake file
   (`docker/devenv-runtime/docker-bake.hcl`)
   to the new `<sha>`, or leave it at `latest` to ride the floating tag.
   The downstream PR is what kubechecks will diff against the gitops
   manifests — kubechecks only fires on consumers that bump their image
   tag in `gitops/`, so the base-image PR itself produces no kubechecks
   diff.

## Open questions / follow-ups

- **Hermes versioning.** ~~The project ships from git only. If/when they cut
  semver tags, switch `HERMES_REF` defaults to a stable tag for prod bakes.~~
  Resolved: upstream publishes calver tags (`vYYYY.M.D`). The base now pins to
  a release tag (`HERMES_REF`) plus the verified commit SHA (`HERMES_COMMIT`)
  with a post-clone `rev-parse HEAD` check in the Dockerfile.
- **ffmpeg / voice mode.** The hermes installer notes voice mode needs
  ffmpeg. Not installed here — voice mode in a headless agent container
  isn't useful. Add `ffmpeg` to the apt list if a future consumer needs it.
- **Unforked multica.** When we move off the agentfarm fork to
  `multica-ai/multica`, this Dockerfile's multica-builder stage flips to a
  `git clone <upstream>@MULTICA_REF` pattern, and this directory can lift
  to `g2crowd/docker-layers/DevOps/agent-runtime-base/` (since it no
  longer needs the agentfarm `server/` tree as context).
