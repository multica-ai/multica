# See docker-bake.backend.hcl for context on the APP_IMAGE / IMG_SHA naming
# (the same caveats about the workflow-level IMAGE_NAME override apply here).
variable "APP_IMAGE" {
  default = "ghcr.io/g2crowd/agentfarm-agentrunner-prod"
}

variable "IMG_SHA" {
  default = "latest"
}

# DEV_TAG is injected by dev.yml as an env var (dev-<full-sha>). Same contract
# as docker-bake.backend.hcl / docker-bake.web.hcl: when set we push a second
# "dev-<sha>" tag so ArgoCD Image Updater on the development environment can
# filter on the "^dev-" prefix without picking up tools/main builds. Empty
# string in prod/tools, where docker/bake-action ignores the empty tag.
variable "DEV_TAG" {
  default = ""
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
  tags       = compact(["${APP_IMAGE}:${IMG_SHA}", DEV_TAG != "" ? "${APP_IMAGE}:${DEV_TAG}" : ""])
  args = {
    BASE_IMAGE = "${BASE_IMAGE}"
    BASE_TAG   = "${BASE_TAG}"
  }
  cache-from = ["type=gha,scope=agentrunner"]
  cache-to   = ["type=gha,scope=agentrunner,mode=max"]
}
