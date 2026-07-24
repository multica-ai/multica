# Changelog

All notable changes to the Multica Helm chart are documented in this file.

## [0.2.0]

### Added

- Production configuration for external PostgreSQL, Redis, SMTP, and
  S3-compatible storage without storing credentials in the chart.
- Arbitrary backend environment variables and environment sources through
  `backend.extraEnv` and `backend.extraEnvFrom`.
- Private registry credentials, immutable image digests, shared ServiceAccount,
  pod and container security contexts, scheduling controls, pod metadata, and
  additional volumes and mounts.
- Configurable backend probes, lifecycle hooks, graceful shutdown, deployment
  strategies, and revision history limits.
- Optional NetworkPolicies, PodDisruptionBudgets, metrics Service, and
  ServiceMonitor.
- Separate frontend and backend Ingress annotations.
- Optional Gateway API HTTPRoutes with common or component-specific references
  to an existing Gateway.
- JSON schema validation for chart values and production deployment
  documentation.

### Compatibility

- Default values continue to render without additional configuration.
- Bundled PostgreSQL, local uploads, Ingress, and the frontend compatibility
  Service named `backend` remain enabled by default.
- NetworkPolicy, PodDisruptionBudget, ServiceMonitor, metrics port, HTTPRoute,
  additional environment variables, additional volumes, and security contexts
  remain opt-in.
