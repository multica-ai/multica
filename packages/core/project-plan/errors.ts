import { ApiError } from "../api/client";

/**
 * Mirrors `projectplan.ErrorKind` (server/internal/projectplan/errors.go).
 * The service raises these; the handler maps them onto HTTP statuses.
 */
export type ProjectPlanErrorKind =
  | "disabled"
  | "invalid"
  | "not_found"
  | "not_active"
  | "active_plan_exists"
  | "version_conflict"
  | "position_conflict"
  | "issue_already_linked"
  | "unavailable"
  | "unknown";

export interface ProjectPlanWriteError {
  kind: ProjectPlanErrorKind;
  /** HTTP status, when the failure came back from the server at all. */
  status: number | null;
  /**
   * The service's own message, taken from the response BODY only — never from
   * `ApiError.message`, which is the synthesized `API error: 404 Not Found`
   * boilerplate the client makes up when the body carried no message. The
   * service's real messages are specific and already user-legible ("another
   * phase already uses this position") and are worth showing verbatim; the
   * boilerplate tells a reader nothing they can act on, so it is dropped and
   * the caller's localized copy is used instead. Empty string when absent.
   */
  message: string;
  /** True when the server said this is a conflict, not a generic failure. */
  conflict: boolean;
}

/** Body shape the handler is expected to use — same `{error, code}` the rest of the API returns. */
function bodyFields(body: unknown): { code?: string; message?: string } {
  if (!body || typeof body !== "object") return {};
  const record = body as Record<string, unknown>;
  const code = typeof record.code === "string" ? record.code : undefined;
  const message =
    typeof record.error === "string"
      ? record.error
      : typeof record.message === "string"
        ? record.message
        : undefined;
  return { code, message };
}

const KIND_BY_CODE = new Set<ProjectPlanErrorKind>([
  "disabled",
  "invalid",
  "not_found",
  "not_active",
  "active_plan_exists",
  "version_conflict",
  "position_conflict",
  "issue_already_linked",
  "unavailable",
]);

/**
 * Classifies a failed plan write.
 *
 * A 409 must read as a conflict rather than as a generic failure. Two
 * independent signals get us there, so the UI stays honest whether or not the
 * handler emits a machine-readable `code`: the
 * body's `code` when present, and the HTTP status otherwise. A network failure
 * with no status at all is `unavailable`, not a conflict — saying "conflict"
 * for a dropped connection would be its own fabrication.
 *
 * `not_found` is deliberately ambiguous, because the status is. Every plan
 * write route is gated on `project_plans` and answers 404 when the flag is
 * off, so a bare 404 means either "this item is gone" or "plans are off in
 * this workspace" and the client cannot tell which. The copy names both rather
 * than picking one and being wrong half the time. (The flag-off case is
 * already unreachable through the UI — no affordance renders — so this only
 * covers a flag flipped mid-session.)
 */
export function classifyPlanWriteError(err: unknown): ProjectPlanWriteError {
  if (!(err instanceof ApiError)) {
    // A non-ApiError never reached the server (DNS, offline, aborted). Its
    // message IS the only signal available, so it is kept.
    return {
      kind: "unavailable",
      status: null,
      message: err instanceof Error && err.message ? err.message : "",
      conflict: false,
    };
  }
  const { code, message } = bodyFields(err.body);
  const serverMessage = message ?? "";

  if (code && KIND_BY_CODE.has(code as ProjectPlanErrorKind)) {
    const kind = code as ProjectPlanErrorKind;
    return {
      kind,
      status: err.status,
      message: serverMessage,
      conflict:
        err.status === 409 ||
        kind === "active_plan_exists" ||
        kind === "version_conflict" ||
        kind === "position_conflict" ||
        kind === "issue_already_linked",
    };
  }

  // No usable `code`: fall back to the status. `409` is the only status that
  // is unambiguously a conflict, so it is the only one promoted as such.
  const kind: ProjectPlanErrorKind =
    err.status === 409
      ? "version_conflict"
      : err.status === 404
        ? "not_found"
        : err.status === 400 || err.status === 422
          ? "invalid"
          : err.status >= 500
            ? "unavailable"
            : "unknown";

  return { kind, status: err.status, message: serverMessage, conflict: err.status === 409 };
}
