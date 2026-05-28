// docker-bake.hcl — multi-arch build for ghcr.io/g2crowd/devenv-runtime.
//
// Layered on ghcr.io/g2crowd/agent-runtime-base; pin BASE_TAG per release.
//
// Build context is this image's own directory (only the entrypoint script
// needs to be COPYed in). Invoke from the agentfarm repo root via the
// reusable g2crowd/gh-actions workflow — it passes
// `files: docker/devenv-runtime/docker-bake.hcl` and sets the workspace as
// the working directory, so the context path below walks from repo root.

variable "REGISTRY"   { default = "ghcr.io" }
variable "REPO"       { default = "g2crowd/devenv-runtime" }
variable "IMG_SHA"    { default = "dev" }
variable "BASE_IMAGE"     { default = "ghcr.io/g2crowd/agent-runtime-base" }
variable "BASE_TAG"       { default = "latest" }
// kubectl — bump in lockstep with EKS control-plane minor.
variable "KUBECTL_MINOR"  { default = "v1.31" }

target "default" {
  context    = "docker/devenv-runtime"
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  // Two tags per build — see the comment in docker/agent-runtime-base/
  // docker-bake.hcl for the rationale (mirrors the same pattern).
  tags = [
    "${REGISTRY}/${REPO}:${IMG_SHA}",
    "${REGISTRY}/${REPO}:latest",
  ]
  args = {
    BASE_IMAGE    = "${BASE_IMAGE}"
    BASE_TAG      = "${BASE_TAG}"
    KUBECTL_MINOR = "${KUBECTL_MINOR}"
  }
  labels = {
    "org.opencontainers.image.source"      = "https://github.com/g2crowd/agentfarm"
    "org.opencontainers.image.description" = "DevEnv runtime — opencode server on the shared agent-runtime base"
    "org.opencontainers.image.licenses"    = "UNLICENSED"
  }
  cache-from = ["type=gha,scope=devenv-runtime"]
  cache-to   = ["type=gha,scope=devenv-runtime,mode=max"]
}
