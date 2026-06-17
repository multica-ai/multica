// @multica/cerebro-workspace-copy — TECH-3582 frontend slice.
//
// A per-item "Copy to workspace" action (and the underlying mutation) that
// copies one entity from the current workspace into another workspace the user
// belongs to, by calling POST /api/workspaces/{id}/cerebro/copy. The copy is
// non-destructive — the source is never modified (see the backend store.go).
//
// Everything is gated behind the cerebro_workspace_copy feature flag.
export * from "./core/types";
export * from "./core/api";
export * from "./core/queries";
export * from "./views";
