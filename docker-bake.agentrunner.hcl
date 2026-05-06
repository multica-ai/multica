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

# Pin the Claude Code npm package version so the image is reproducible.
# Bump this alongside the multica CLI when claude-code changes its CLI surface.
variable "CLAUDE_CODE_VERSION" {
  default = "latest"
}

target "default" {
  context    = "."
  dockerfile = "Dockerfile.agentrunner"
  platforms  = ["linux/arm64"]
  tags       = compact(["${APP_IMAGE}:${IMG_SHA}", DEV_TAG != "" ? "${APP_IMAGE}:${DEV_TAG}" : ""])
  args = {
    CLAUDE_CODE_VERSION = "${CLAUDE_CODE_VERSION}"
  }
  cache-from = ["type=gha,scope=agentrunner"]
  cache-to   = ["type=gha,scope=agentrunner,mode=max"]
}
