# syntax=docker/dockerfile:1.7
#
# devenv-runtime — opencode server running on the shared agent-runtime base.
#
# Replaces the install-everything `opencode-dev` image in g2crowd/docker-layers
# (out of scope for this POC to cut DevEnv production over — keep both for a
# release; cut over once stable per PLA-267).

ARG BASE_IMAGE=ghcr.io/g2crowd/agent-runtime-base
ARG BASE_TAG=latest

FROM ${BASE_IMAGE}:${BASE_TAG}

USER root
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN sed -i 's/\r$//' /usr/local/bin/entrypoint.sh \
 && chmod +x /usr/local/bin/entrypoint.sh

USER agent

# opencode serve binds 127.0.0.1 by default. We override with 0.0.0.0 in the
# entrypoint so the Kubernetes Service can reach the pod. Default port 4096
# matches the upstream `opencode serve` default; downstream Kustomize sets
# OPENCODE_PORT if it needs a different port.
ENV OPENCODE_HOST=0.0.0.0 \
    OPENCODE_PORT=4096

EXPOSE 4096

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
