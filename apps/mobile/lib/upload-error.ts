/**
 * Upload refusals the server reports with a machine-readable code. Mirrors
 * `storageLimitReachedFromError` in `packages/core/billing/storage-limit.ts`
 * (MUL-9489): a full workspace refuses new uploads with
 * `403 storage_limit_reached`. Mobile shows a localized message for it and,
 * unlike web/desktop, offers no upgrade action.
 */
export const STORAGE_LIMIT_REACHED_CODE = "storage_limit_reached";

export function isStorageLimitReached(error: unknown): boolean {
  if (typeof error !== "object" || error === null) return false;
  const { status, body } = error as { status?: unknown; body?: unknown };
  return (
    status === 403 &&
    typeof body === "object" &&
    body !== null &&
    (body as { code?: unknown }).code === STORAGE_LIMIT_REACHED_CODE
  );
}
