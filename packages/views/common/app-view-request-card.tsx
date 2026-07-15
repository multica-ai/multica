"use client";

// CEREBRO-PATCH(cerebro-mini-app-complete-surface): FIR-3172 request-bound interactive app card.

import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@multica/core/api";
import { useWorkspaceSlug } from "@multica/core/paths";

type ViewRequest = {
  id: string;
  app_id: string;
  app_name: string;
  app_version: string;
  view_id: string;
  input: unknown;
  status: "waiting" | "submitted";
  runtime_url: string;
};

export function AppViewRequestCard({ requestId }: { requestId: string }) {
  const workspaceSlug = useWorkspaceSlug();
  const frame = useRef<HTMLIFrameElement>(null);
  const [request, setRequest] = useState<ViewRequest | null>(null);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!workspaceSlug) return;
    let active = true;
    const endpoint = `/api/cerebro/apps/view-requests/${encodeURIComponent(requestId)}?workspace_slug=${encodeURIComponent(workspaceSlug)}`;
    api.cerebroRequest<ViewRequest>(endpoint)
      .then((value) => active && setRequest(value))
      .catch((cause: unknown) => active && setError(cause instanceof Error ? cause.message : "This app view is unavailable."));
    return () => { active = false; };
  }, [requestId, workspaceSlug]);

  const submit = useCallback(async (value: unknown) => {
    if (!request || request.status !== "waiting" || !workspaceSlug) return;
    setSubmitting(true);
    setError("");
    try {
      const endpoint = `/api/cerebro/apps/${encodeURIComponent(request.app_id)}/views/${encodeURIComponent(request.view_id)}/submissions?workspace_slug=${encodeURIComponent(workspaceSlug)}`;
      await api.cerebroRequest(endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ request_id: request.id, version: request.app_version, value }),
      });
      setRequest((current) => current ? { ...current, status: "submitted" } : current);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "The response could not be submitted.");
    } finally {
      setSubmitting(false);
    }
  }, [request, workspaceSlug]);

  useEffect(() => {
    if (!request) return;
    const allowedOrigin = new URL(request.runtime_url, window.location.href).origin;
    const receive = (event: MessageEvent) => {
      // Without allow-same-origin the sandbox intentionally reports an opaque
      // "null" origin. Window identity is the security boundary here.
      if (event.source !== frame.current?.contentWindow || (event.origin !== allowedOrigin && event.origin !== "null")) return;
      if (event.data?.type === "multica.app-view.submit") void submit(event.data.value);
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [request, submit]);

  return (
    <section data-testid={`app-view-request-${requestId}`} className="not-prose my-2 overflow-hidden rounded-xl border bg-background">
      <header className="border-b px-4 py-3 text-sm font-medium">
        {request?.app_name ?? "Interactive app"}
        {submitting ? <span className="ml-2 text-muted-foreground">Submitting…</span> : null}
        {request?.status === "submitted" ? <span className="ml-2 text-muted-foreground">Submitted</span> : null}
      </header>
      {error ? <p className="px-4 py-3 text-sm text-destructive">{error}</p> : null}
      {!request && !error ? <p className="px-4 py-3 text-sm text-muted-foreground">Loading app…</p> : null}
      {request ? (
        <iframe
          ref={frame}
          src={request.runtime_url}
          title={`${request.app_name}: ${request.view_id}`}
          sandbox="allow-scripts allow-forms"
          onLoad={() => frame.current?.contentWindow?.postMessage({ type: "multica.app-view.init", input: request.input, request_id: request.id }, "*")}
          className="min-h-64 w-full bg-background"
        />
      ) : null}
    </section>
  );
}
