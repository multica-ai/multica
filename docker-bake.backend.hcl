# NOTE: Do not use the variable name `IMAGE_NAME` here. The reusable workflow
# `g2crowd/gh-actions/.github/workflows/gitops-bake-build.yml` sets a
# workflow-level `env: IMAGE_NAME: ${{ github.repository }}` which
# docker/bake-action automatically surfaces as an HCL variable override,
# silently replacing any IMAGE_NAME default declared here. That caused pushes
# to go to `g2crowd/agentfarm:latest` (Docker Hub) instead of GHCR.
# Using a distinct variable name (APP_IMAGE_BASE) avoids the collision.
#
# The final image name is `${APP_IMAGE_BASE}-${ENVIRONMENT}` so that
# publish.yml (environment=prod) keeps pushing to agentfarm-backend-prod and
# dev.yml (environment=dev) pushes to agentfarm-backend-dev. This is the
# cleanest way to discriminate dev from prod builds — `ENVIRONMENT` is one of
# the few env vars the reusable workflow forwards through the workflow_call
# boundary (see gitops-bake-build.yml `env:` block on the bake step), so we
# don't need to modify the gh-actions reusable workflow or coordinate
# anything cross-repo. ArgoCD Image Updater on the dev Application points at
# the -dev repo and picks newest SHA, no tag-prefix regex needed.
variable "APP_IMAGE_BASE" {
  default = "ghcr.io/g2crowd/agentfarm-backend"
}

# ENVIRONMENT is forwarded from the reusable workflow's `inputs.environment`
# (set to "prod" by publish.yml and "dev" by dev.yml). Default to "prod" so a
# bare `docker buildx bake` from a developer's laptop doesn't accidentally
# stamp a dev image.
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
  dockerfile = "Dockerfile"
  platforms  = ["linux/arm64"]
  tags       = ["${APP_IMAGE_BASE}-${ENVIRONMENT}:${IMG_SHA}"]
  cache-from = ["type=gha,scope=backend"]
  cache-to   = ["type=gha,scope=backend,mode=max"]
}
