export const DIAGNOSTICS_EXPORT_CHANNEL = "daemon:export-diagnostics";

export type DiagnosticsExportResult =
  | { status: "saved" | "cancelled" | "busy" }
  | { status: "error"; reason: "profile_changed" | "save_failed" };
