// docker-bake.hcl — build for ghcr.io/g2crowd/agentrunner-runtime.
//
// Layered on ghcr.io/g2crowd/devenv-runtime until the agentrunner image is
// dissociated from devenv. Invoke from the agentfarm repo root via the
// reusable g2crowd/gh-actions workflow with
// `files: docker/agentrunner-runtime/docker-bake.hcl`.

variable "REGISTRY"      { default = "ghcr.io" }
variable "REPO"          { default = "g2crowd/agentrunner-runtime" }
variable "IMG_SHA"       { default = "dev" }
variable "DEVENV_IMAGE"  { default = "ghcr.io/g2crowd/devenv-runtime" }
variable "DEVENV_TAG"    { default = "latest" }

target "default" {
  context    = "docker/agentrunner-runtime"
  dockerfile = "Dockerfile"
  platforms  = ["linux/amd64", "linux/arm64"]
  tags = [
    "${REGISTRY}/${REPO}:${IMG_SHA}",
    "${REGISTRY}/${REPO}:latest",
  ]
  args = {
    DEVENV_IMAGE = "${DEVENV_IMAGE}"
    DEVENV_TAG   = "${DEVENV_TAG}"
  }
  labels = {
    "org.opencontainers.image.source"      = "https://github.com/g2crowd/agentfarm"
    "org.opencontainers.image.description" = "Agentrunner runtime — cloud agent runner (layered on devenv-runtime until dissociation)"
    "org.opencontainers.image.licenses"    = "UNLICENSED"
  }
  cache-from = ["type=gha,scope=agentrunner-runtime"]
  cache-to   = ["type=gha,scope=agentrunner-runtime,mode=max"]
}
