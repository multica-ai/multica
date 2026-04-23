variable "IMAGE_NAME" {
  default = "ghcr.io/g2crowd/agentfarm-backend-prod"
}

variable "TAG" {
  default = "latest"
}

target "default" {
  context    = "."
  dockerfile = "Dockerfile"
  platforms  = ["linux/arm64"]
  tags       = ["${IMAGE_NAME}:${TAG}"]
  cache-from = ["type=gha,scope=backend"]
  cache-to   = ["type=gha,scope=backend,mode=max"]
}
