export const RENDERER_ROUTE_CONTEXT_CHANNEL = "renderer:route-context";

export type RendererRouteSurface = "login" | "overlay" | "tab";

export type RendererRouteContextInput = {
  surface: RendererRouteSurface;
  path: string;
  workspaceSlug?: string;
  tabId?: string;
};

export type RendererRouteContext = RendererRouteContextInput & {
  reportedAt: string;
};

const MAX_ROUTE_CONTEXT_STRING_LENGTH = 512;

export function sanitizeRendererRouteContext(
  value: unknown,
  reportedAt = new Date(),
): RendererRouteContext | null {
  if (!value || typeof value !== "object") return null;

  const input = value as Record<string, unknown>;
  if (!isRendererRouteSurface(input.surface)) return null;

  const path = sanitizeString(input.path);
  if (!path) return null;

  const workspaceSlug = sanitizeString(input.workspaceSlug);
  const tabId = sanitizeString(input.tabId);

  return {
    surface: input.surface,
    path,
    ...(workspaceSlug ? { workspaceSlug } : {}),
    ...(tabId ? { tabId } : {}),
    reportedAt: reportedAt.toISOString(),
  };
}

function isRendererRouteSurface(value: unknown): value is RendererRouteSurface {
  return value === "login" || value === "overlay" || value === "tab";
}

function sanitizeString(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  return trimmed.slice(0, MAX_ROUTE_CONTEXT_STRING_LENGTH);
}
