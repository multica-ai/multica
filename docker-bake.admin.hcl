# Same env-suffix scheme as docker-bake.web.hcl / docker-bake.backend.hcl —
# see the long comment in docker-bake.backend.hcl for the full rationale.
# Short version: publish.yml passes environment=prod, dev.yml (if ever wired
# for admin) would pass environment=dev, and the bake target composes
# `${APP_IMAGE_BASE}-${ENVIRONMENT}` to land in the right GHCR repo.
#
# Also uses APP_IMAGE_BASE (not IMAGE_NAME) to avoid the workflow-level
# IMAGE_NAME env collision documented in docker-bake.web.hcl.
variable "APP_IMAGE_BASE" {
  default = "ghcr.io/g2crowd/agentfarm-admin"
}

variable "ENVIRONMENT" {
  default = "prod"
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

target "default" {
  context    = "."
  dockerfile = "Dockerfile.admin"
  platforms  = ["linux/arm64"]
  tags       = ["${APP_IMAGE_BASE}-${ENVIRONMENT}:${IMG_SHA}"]
  cache-from = ["type=gha,scope=admin"]
  cache-to   = ["type=gha,scope=admin,mode=max"]
}
