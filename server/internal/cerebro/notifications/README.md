# server/internal/cerebro/notifications

Cerebro-fork notifications feature backend (notifications tab, settings, delivery wiring).

**Owns:** Notification preferences, delivery hooks, notification-related projections.

**May land here:** User-facing notification settings, delivery dispatchers tied to cerebro events.

**May NOT land here:** Upstream realtime/inbox primitives (lives in `server/internal/realtime` + `server/internal/handler/inbox*`).

**Naming:** snake_case files.

**Upstream-import-niveau:** L4 (cerebro-only).
