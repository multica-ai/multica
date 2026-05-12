// docker-bake.hcl — multi-arch build for ghcr.io/g2crowd/agent-runtime-base.
//
// Follows the same target="default" + IMG_SHA conventions as
// docker-bake.agentrunner.hcl so the reusable g2crowd/gh-actions workflow
// can drive this build without bespoke wiring.
//
// Multi-arch: linux/amd64 + linux/arm64. Both DevEnv (graviton arm64 nodes)
// and the current agentrunner (arm64) need at least arm64; amd64 is included
// because every other piece of the tools-cluster CI runs on amd64 GH runners.
//
// Build context: the agentfarm repo root. Invoke this bake file from the
// repo root, the same way docker-bake.agentrunner.hcl is invoked:
//
//   docker buildx bake -f docker/agent-runtime-base/docker-bake.hcl default
//
// The multica builder stage COPYs out of server/ directly, so the context
// MUST be the repo root. The repo's top-level .dockerignore is what scopes
// the upload.

variable "REGISTRY" {
  default = "ghcr.io"
}

variable "REPO" {
  default = "g2crowd/agent-runtime-base"
}

variable "IMG_SHA" {
  default = "dev"
}

// Pin args. Defaults match what's in the Dockerfile so a bare
// `docker buildx bake` works locally; override in CI per release.
variable "NODE_VERSION"        { default = "22" }
variable "GO_VERSION"          { default = "1.26.1" }
variable "MULTICA_VERSION"     { default = "dev" }
variable "MULTICA_COMMIT"      { default = "unknown" }
variable "CLAUDE_CODE_VERSION" { default = "latest" }
variable "CODEX_VERSION"       { default = "latest" }
variable "OPENCODE_VERSION"    { default = "latest" }
variable "PI_VERSION"          { default = "latest" }
// Pinned to an upstream calver release tag (vYYYY.M.D) + the commit SHA
// that tag resolves to (peeled, i.e. what `git rev-parse HEAD` returns
// after `git clone --branch <tag>`). The Dockerfile verifies they match
// post-clone — build fails if the tag is force-moved. Bump both together
// via the README's "Bump procedure". Never default HERMES_REF back to a
// branch name like `main`.
variable "HERMES_REF"          { default = "v2026.5.7" }
variable "HERMES_COMMIT"       { default = "498bfc7bc12a937621b4215312049b1000726df3" }
// gh CLI apt version pin (e.g. "2.63.0"). Empty = whatever cli.github.com
// ships at build time; pin for stricter reproducibility once a known-good
// version is identified.
variable "GH_CLI_VERSION"      { default = "" }
variable "UID"                 { default = "1000" }
variable "GID"                 { default = "1000" }

target "default" {
  context    = "."
  dockerfile = "docker/agent-runtime-base/Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  // Two tags per build:
  //   <REPO>:<IMG_SHA>  — immutable; downstream consumers can pin to this
  //   <REPO>:latest     — mutable; what the agentrunner / devenv-runtime
  //                       bake files FROM by default until they bump
  //                       BASE_TAG to a specific SHA.
  //
  // CI splits this into per-arch builds via the `default-amd64` /
  // `default-arm64` targets below (each pinned to one platform + an
  // arch-suffixed tag), then `docker buildx imagetools create` stitches
  // the per-arch manifests under the canonical `:${IMG_SHA}` / `:latest`
  // tags. This target stays multi-arch so a bare local `docker buildx
  // bake` (under emulation) still works for developers.
  tags = [
    "${REGISTRY}/${REPO}:${IMG_SHA}",
    "${REGISTRY}/${REPO}:latest",
  ]
  args = {
    NODE_VERSION        = "${NODE_VERSION}"
    GO_VERSION          = "${GO_VERSION}"
    MULTICA_VERSION     = "${MULTICA_VERSION}"
    MULTICA_COMMIT      = "${MULTICA_COMMIT}"
    CLAUDE_CODE_VERSION = "${CLAUDE_CODE_VERSION}"
    CODEX_VERSION       = "${CODEX_VERSION}"
    OPENCODE_VERSION    = "${OPENCODE_VERSION}"
    PI_VERSION          = "${PI_VERSION}"
    HERMES_REF          = "${HERMES_REF}"
    HERMES_COMMIT       = "${HERMES_COMMIT}"
    GH_CLI_VERSION      = "${GH_CLI_VERSION}"
    UID                 = "${UID}"
    GID                 = "${GID}"
  }
  labels = {
    "org.opencontainers.image.source"      = "https://github.com/g2crowd/agentfarm"
    "org.opencontainers.image.description" = "Shared agent-runtime base (multica + claude + codex + opencode + pi + hermes)"
    "org.opencontainers.image.licenses"    = "UNLICENSED"
  }
  cache-from = ["type=gha,scope=agent-runtime-base"]
  cache-to   = ["type=gha,scope=agent-runtime-base,mode=max"]
}

// Per-arch targets for CI's native-runner matrix. Each inherits everything
// from `default` then overrides the single-arch platform + arch-suffixed
// tags + an arch-scoped cache (so the two matrix legs don't clobber each
// other's cache). The CI merge job consumes these arch-suffixed tags and
// re-tags the combined manifest as `:${IMG_SHA}` / `:latest`.
target "default-amd64" {
  inherits   = ["default"]
  platforms  = ["linux/amd64"]
  tags = [
    "${REGISTRY}/${REPO}:${IMG_SHA}-amd64",
  ]
  cache-from = ["type=gha,scope=agent-runtime-base-amd64"]
  cache-to   = ["type=gha,scope=agent-runtime-base-amd64,mode=max"]
}

target "default-arm64" {
  inherits   = ["default"]
  platforms  = ["linux/arm64"]
  tags = [
    "${REGISTRY}/${REPO}:${IMG_SHA}-arm64",
  ]
  cache-from = ["type=gha,scope=agent-runtime-base-arm64"]
  cache-to   = ["type=gha,scope=agent-runtime-base-arm64,mode=max"]
}
