export const DIAGNOSTICS_EXPORT_CHANNEL = "daemon:export-diagnostics";

export type DiagnosticsExportResult =
  | { status: "saved"; partial: boolean }
  | { status: "cancelled" | "busy" }
  | { status: "error"; reason: "profile_changed" | "save_failed" };
