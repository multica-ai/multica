# NOTE: Do not use the variable name `IMAGE_NAME` here. The reusable workflow
# `g2crowd/gh-actions/.github/workflows/gitops-bake-build.yml` sets a
# workflow-level `env: IMAGE_NAME: ${{ github.repository }}` which
# docker/bake-action automatically surfaces as an HCL variable override,
# silently replacing any IMAGE_NAME default declared here. That caused pushes
# to go to `g2crowd/agentfarm:latest` (Docker Hub) instead of GHCR.
# Using a distinct variable name (APP_IMAGE) avoids the collision.
variable "APP_IMAGE" {
  default = "ghcr.io/g2crowd/agentfarm-web-prod"
}

# IMG_SHA is injected by the reusable workflow
# (g2crowd/gh-actions/.github/workflows/gitops-bake-build.yml) as an env var,
# which docker/bake-action surfaces as an HCL variable override. The workflow
# sets it to the first 8 chars of the commit SHA so every build produces a
# unique image tag — required for ArgoCD to detect a diff in
# gitops/environments/tools/kustomization.yaml and trigger a sync.
variable "IMG_SHA" {
  default = "latest"
}

variable "REMOTE_API_URL" {
  default = "http://agentfarm-backend:8080"
}

target "default" {
  context    = "."
  dockerfile = "Dockerfile.web"
  platforms  = ["linux/arm64"]
  tags       = ["${APP_IMAGE}:${IMG_SHA}"]
  args = {
    REMOTE_API_URL = "${REMOTE_API_URL}"
  }
  cache-from = ["type=gha,scope=web"]
  cache-to   = ["type=gha,scope=web,mode=max"]
}
