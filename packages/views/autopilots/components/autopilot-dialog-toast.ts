import type { TFunction } from "i18next";

// Centralizes the partial-success toast formatting so the i18n keys and the
// `{ reason }` placeholder live in one tested place. Without this, the
// translation contract in `autopilot-dialog-i18n.test.ts` could pass while
// the dialog's call-site silently passes the wrong variable name and ships
// a literal `{{reason}}` to users.
export function formatSchedulePartialFailureToast(
  t: TFunction<"autopilots">,
  kind: "create" | "update",
  reason: string | null,
): string {
  if (reason) {
    return kind === "create"
      ? t(($) => $.dialog.toast_create_partial_with_reason, { reason })
      : t(($) => $.dialog.toast_update_partial_with_reason, { reason });
  }
  return kind === "create"
    ? t(($) => $.dialog.toast_create_partial)
    : t(($) => $.dialog.toast_update_partial);
}
