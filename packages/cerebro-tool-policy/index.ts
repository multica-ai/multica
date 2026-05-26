// @multica/cerebro-tool-policy — the viewless data layer + table component for
// the FIR-2230 unified per-tool permission chain (Runtime › Agent › Group ›
// User). Extracted into its own leaf package so agent-, runtime-, and
// permission-views can all consume the table without importing @multica/views
// (which would close a package dependency cycle).
export * from "./core";
export * from "./views";
