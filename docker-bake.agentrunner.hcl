# See docker-bake.backend.hcl for the full rationale on the
# APP_IMAGE_BASE / ENVIRONMENT split (the same caveats about the workflow-level
# IMAGE_NAME override apply here). Short version: publish.yml sets
# environment=prod → agentfarm-agentrunner-prod; dev.yml sets environment=dev
# → agentfarm-agentrunner-dev. Image Updater on dev points at the -dev repo.
variable "APP_IMAGE_BASE" {
  default = "ghcr.io/g2crowd/agentfarm-agentrunner"
}

variable "ENVIRONMENT" {
  default = "prod"
}

variable "IMG_SHA" {
  default = "latest"
}

# The agentrunner image is now a thin layer on top of
# ghcr.io/g2crowd/agent-runtime-base (see docker/agent-runtime-base/). All
# tool versions (claude, codex, opencode, pi, hermes, multica) are pinned in
# that base image — bump them by rebuilding the base, then bumping BASE_TAG
# here.
variable "BASE_IMAGE" {
  default = "ghcr.io/g2crowd/agent-runtime-base"
}

variable "BASE_TAG" {
  default = "latest"
}

target "default" {
  context    = "."
  dockerfile = "Dockerfile.agentrunner"
  # Multi-arch. amd64 was added when the base image went multi-arch; the
  # agentrunner deployment.yaml still pins arm64 nodes (graviton Karpenter
  # pool) but the image being multi-arch lets local dev on amd64 laptops
  # work without QEMU.
  platforms  = ["linux/amd64", "linux/arm64"]
  tags       = ["${APP_IMAGE_BASE}-${ENVIRONMENT}:${IMG_SHA}"]
  args = {
    BASE_IMAGE = "${BASE_IMAGE}"
    BASE_TAG   = "${BASE_TAG}"
  }
  cache-from = ["type=gha,scope=agentrunner"]
  cache-to   = ["type=gha,scope=agentrunner,mode=max"]
}
