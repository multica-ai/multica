# NOTE: Do not use the variable name `IMAGE_NAME` here. The reusable workflow
# `g2crowd/gh-actions/.github/workflows/gitops-bake-build.yml` sets a
# workflow-level `env: IMAGE_NAME: ${{ github.repository }}` which
# docker/bake-action automatically surfaces as an HCL variable override,
# silently replacing any IMAGE_NAME default declared here. Same trap as
# docker-bake.backend.hcl / docker-bake.web.hcl — keep APP_IMAGE.
variable "APP_IMAGE" {
  default = "ghcr.io/g2crowd/agentfarm-daemon-prod"
}

# IMG_SHA is injected by the reusable workflow as an env var, surfaced as
# an HCL variable override. First 8 chars of the commit SHA so every build
# produces a unique tag — required for ArgoCD to detect a diff in
# gitops/environments/tools/kustomization.yaml and trigger a sync.
variable "IMG_SHA" {
  default = "latest"
}

# This bake file is intentionally separate from docker-bake.backend.hcl:
# the daemon image picks up CLI version bumps (claude-code, codex) on its
# own cadence, and shouldn't trigger a backend rebuild every time. See
# docs/proposals/per-workspace-daemon-runtimes.md §3.
target "default" {
  context    = "."
  dockerfile = "Dockerfile.daemon"
  platforms  = ["linux/arm64"]
  tags       = ["${APP_IMAGE}:${IMG_SHA}"]
  cache-from = ["type=gha,scope=daemon"]
  cache-to   = ["type=gha,scope=daemon,mode=max"]
}
