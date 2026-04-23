# NOTE: Do not use the variable name `IMAGE_NAME` here. The reusable workflow
# `g2crowd/gh-actions/.github/workflows/gitops-bake-build.yml` sets a
# workflow-level `env: IMAGE_NAME: ${{ github.repository }}` which
# docker/bake-action automatically surfaces as an HCL variable override,
# silently replacing any IMAGE_NAME default declared here. That caused pushes
# to go to `g2crowd/agentfarm:latest` (Docker Hub) instead of GHCR.
# Using a distinct variable name (APP_IMAGE) avoids the collision.
variable "APP_IMAGE" {
  default = "ghcr.io/g2crowd/agentfarm-backend-prod"
}

variable "TAG" {
  default = "latest"
}

target "default" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/arm64"]
  tags       = ["${APP_IMAGE}:${TAG}"]
  cache-from = ["type=gha,scope=backend"]
  cache-to   = ["type=gha,scope=backend,mode=max"]
}
