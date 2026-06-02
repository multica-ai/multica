import { useMemo } from "react";
import { useLocation, useNavigate, useRouteError } from "react-router-dom";
import { AlertTriangle, RotateCw, Send, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useModalStore } from "@multica/core/modals";
import { useTabStore } from "@/stores/tab-store";

type DesktopAppInfo = {
  version?: string;
  os?: string;
};

export function formatRouteErrorReport({
  error,
  url,
  appInfo,
  trigger,
}: {
  error: unknown;
  url: string;
  appInfo?: DesktopAppInfo;
  trigger: string;
}) {
  const normalized = normalizeError(error);
  return [
    "kind: desktop_route_error",
    `trigger: ${trigger}`,
    `url: ${url}`,
    `app_version: ${appInfo?.version ?? "unknown"}`,
    `runtime_os: ${appInfo?.os ?? "unknown"}`,
    "",
    "context:",
    `- name: ${normalized.name}`,
    `- message: ${normalized.message}`,
    "",
    "stack:",
    "```",
    normalized.stack ?? "<no stack>",
    "```",
    "",
    "TODO: promote kind/context to structured feedback fields once the feedback API supports them.",
  ].join("\n");
}

export function DesktopRouteErrorPage() {
  const error = useRouteError();
  const location = useLocation();
  const navigate = useNavigate();
  const workspaceSlug = location.pathname.split("/").filter(Boolean)[0];
  const safeRoute = workspaceSlug ? `/${workspaceSlug}/issues` : null;
  const report = useMemo(
    () =>
      formatRouteErrorReport({
        error,
        url:
          typeof window !== "undefined"
            ? `${window.location.origin}${location.pathname}${location.search}${location.hash}`
            : location.pathname,
        appInfo: typeof window !== "undefined" ? window.desktopAPI?.appInfo : undefined,
        trigger: "route-errorElement",
      }),
    [error, location.hash, location.pathname, location.search],
  );
  const message = normalizeError(error).message;

  return (
    <div
      role="alert"
      className="flex h-full min-h-[20rem] flex-col items-center justify-center gap-4 p-8 text-center"
    >
      <div className="rounded-full bg-destructive/10 p-3 text-destructive">
        <AlertTriangle className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-2">
        <h2 className="text-lg font-semibold">Something went wrong in this tab</h2>
        <p className="max-w-lg text-sm text-muted-foreground">
          A route-level renderer error was contained before it could take down the
          desktop shell. Reload this tab, or send the report if it keeps happening.
        </p>
        <p className="max-w-lg truncate text-xs text-muted-foreground">{message}</p>
      </div>
      <div className="flex gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={() => useTabStore.getState().reloadActiveTab()}
        >
          <RotateCw className="mr-2 h-4 w-4" aria-hidden="true" />
          Reload tab
        </Button>
        {safeRoute ? (
          <Button type="button" variant="outline" onClick={() => navigate(safeRoute, { replace: true })}>
            Go to issues
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          onClick={() => useTabStore.getState().closeActiveTab()}
        >
          <X className="mr-2 h-4 w-4" aria-hidden="true" />
          Close tab
        </Button>
        <Button
          type="button"
          onClick={() =>
            useModalStore.getState().open("feedback", {
              initialMessage: report,
            })
          }
        >
          <Send className="mr-2 h-4 w-4" aria-hidden="true" />
          Report error
        </Button>
      </div>
    </div>
  );
}

function normalizeError(error: unknown): { name: string; message: string; stack?: string } {
  if (error instanceof Error) {
    return {
      name: error.name || "Error",
      message: error.message || "Unknown route error",
      stack: error.stack,
    };
  }
  if (typeof error === "string") {
    return { name: "Error", message: error };
  }
  return { name: "Error", message: "Unknown route error", stack: safeJson(error) };
}

function safeJson(value: unknown) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}