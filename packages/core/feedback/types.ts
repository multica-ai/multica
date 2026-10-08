export const FEEDBACK_KINDS = ["bug", "feature", "general", "praise"] as const;

export type FeedbackKind = (typeof FEEDBACK_KINDS)[number];

export interface FeedbackErrorContext {
  name: string;
  message: string;
  stack?: string;
}

export interface DesktopRouteErrorFeedbackContext {
  kind: "desktop_route_error";
  trigger: string;
  error: FeedbackErrorContext;
}

export type FeedbackContext = DesktopRouteErrorFeedbackContext;

export function isFeedbackContext(value: unknown): value is FeedbackContext {
  if (!value || typeof value !== "object") return false;
  const context = value as Record<string, unknown>;
  if (
    context.kind !== "desktop_route_error" ||
    typeof context.trigger !== "string" ||
    !context.error ||
    typeof context.error !== "object"
  ) {
    return false;
  }
  const error = context.error as Record<string, unknown>;
  return (
    typeof error.name === "string" &&
    typeof error.message === "string" &&
    (error.stack === undefined || typeof error.stack === "string")
  );
}

export interface CreateFeedbackResponse {
  id: string;
  created_at: string;
}
