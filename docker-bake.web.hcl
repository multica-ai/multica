# NOTE: Do not use the variable name `IMAGE_NAME` here. The reusable workflow
# `g2crowd/gh-actions/.github/workflows/gitops-bake-build.yml` sets a
# workflow-level `env: IMAGE_NAME: ${{ github.repository }}` which
# docker/bake-action automatically surfaces as an HCL variable override,
# silently replacing any IMAGE_NAME default declared here. That caused pushes
# to go to `g2crowd/agentfarm:latest` (Docker Hub) instead of GHCR.
# Using a distinct variable name (APP_IMAGE_BASE) avoids the collision.
#
# Same env-suffix scheme as docker-bake.backend.hcl — see the long comment
# there for the full rationale. Short version: publish.yml passes
# environment=prod, dev.yml passes environment=dev, and the bake target
# composes `${APP_IMAGE_BASE}-${ENVIRONMENT}` to land in the right GHCR repo.
variable "APP_IMAGE_BASE" {
  default = "ghcr.io/g2crowd/agentfarm-web"
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

variable "REMOTE_API_URL" {
  default = "http://agentfarm-backend:8080"
}

# NEXT_PUBLIC_GOOGLE_CLIENT_ID is baked into the Next.js client bundle at
# build time (NEXT_PUBLIC_* is inlined by Next at `pnpm build`, not read at
# runtime). The SSM-sourced Secret reaches pods too late for this — it would
# only populate server-side env. Google OAuth client IDs are public (they're
# visible in every browser's network tab), so defaulting here is correct;
# the backend-only GOOGLE_CLIENT_SECRET stays in SSM → agentfarm-secret-store.
variable "NEXT_PUBLIC_GOOGLE_CLIENT_ID" {
  default = "123139273422-1tj8oe3ar270m5k92r4g6pqhivep2j03.apps.googleusercontent.com"
}

# Host shown in onboarding workspace-URL previews (the `{host}/[slug]` pill
# in step 2, existing-workspace card, sidebar preview). NEXT_PUBLIC_* — same
# build-time-inlining constraint as NEXT_PUBLIC_GOOGLE_CLIENT_ID above, so
# defaulted here rather than in SSM. Non-secret (visible in every browser).
variable "NEXT_PUBLIC_WORKSPACE_URL_HOST" {
  default = "agentfarm.g2.com"
}

target "default" {
  context    = "."
  dockerfile = "Dockerfile.web"
  platforms  = ["linux/arm64"]
  tags       = ["${APP_IMAGE_BASE}-${ENVIRONMENT}:${IMG_SHA}"]
  args = {
    REMOTE_API_URL                 = "${REMOTE_API_URL}"
    NEXT_PUBLIC_GOOGLE_CLIENT_ID   = "${NEXT_PUBLIC_GOOGLE_CLIENT_ID}"
    NEXT_PUBLIC_WORKSPACE_URL_HOST = "${NEXT_PUBLIC_WORKSPACE_URL_HOST}"
  }
  cache-from = ["type=gha,scope=web"]
  cache-to   = ["type=gha,scope=web,mode=max"]
}
