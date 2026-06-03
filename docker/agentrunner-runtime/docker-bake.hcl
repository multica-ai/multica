// docker-bake.hcl — build for ghcr.io/g2crowd/agentrunner-runtime.
//
// Layered directly on ghcr.io/g2crowd/agent-runtime-base (Phase-1 strategy A
// dissociation: agentrunner no longer FROMs devenv-runtime).
//
// Invoked via the reusable g2crowd/gh-actions workflow with
//   files: docker/agentrunner-runtime/docker-bake.hcl

variable "REGISTRY"   { default = "ghcr.io" }
variable "REPO"       { default = "g2crowd/agentrunner-runtime" }
variable "IMG_SHA"    { default = "dev" }
variable "BASE_IMAGE" { default = "ghcr.io/g2crowd/agent-runtime-base" }
variable "BASE_TAG"   { default = "latest" }

target "default" {
  context    = "docker/agentrunner-runtime"
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags = [
    "${REGISTRY}/${REPO}:${IMG_SHA}",
    "${REGISTRY}/${REPO}:latest",
  ]
  args = {
    BASE_IMAGE = "${BASE_IMAGE}"
    BASE_TAG   = "${BASE_TAG}"
  }
  labels = {
    "org.opencontainers.image.source"      = "https://github.com/g2crowd/agentfarm"
    "org.opencontainers.image.description" = "Agentrunner runtime — headless cloud agent runner on the shared agent-runtime base"
    "org.opencontainers.image.licenses"    = "UNLICENSED"
  }
  cache-from = ["type=gha,scope=agentrunner-runtime"]
  cache-to   = ["type=gha,scope=agentrunner-runtime,mode=max"]
}
