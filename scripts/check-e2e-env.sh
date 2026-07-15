#!/usr/bin/env bash

# The mini-app E2E seeds its workspace flag, but the server also has a
# default-off process-level floor. Full checks must enable both layers.
export CEREBRO_MINI_APPS_ENABLED=true
