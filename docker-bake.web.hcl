variable "IMAGE_NAME" {
  default = "ghcr.io/g2crowd/agentfarm-web-prod"
}

variable "TAG" {
  default = "latest"
}

variable "REMOTE_API_URL" {
  default = "http://agentfarm-backend:8080"
}

target "default" {
  context    = "."
  dockerfile = "Dockerfile.web"
  platforms  = ["linux/arm64"]
  tags       = ["${IMAGE_NAME}:${TAG}"]
  args = {
    REMOTE_API_URL = "${REMOTE_API_URL}"
  }
  cache-from = ["type=gha,scope=web"]
  cache-to   = ["type=gha,scope=web,mode=max"]
}
