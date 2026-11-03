"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2, Pencil, Plug, ShieldCheck } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  chatAppRequestsOptions,
  issueAppRequestsOptions,
  useDecideAppRequest,
} from "@multica/core/connected-apps";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AppRequest } from "@multica/core/types";
import { useLocale, useT } from "../i18n";

/** Pending app confirmations of an issue, shown above its composer. */
export function IssueAppRequests({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const { data } = useQuery({ ...issueAppRequestsOptions(wsId, issueId), enabled });
  return <AppRequestList requests={data ?? []} />;
}

/** Pending app confirmations of a chat, shown above its input. */
export function ChatAppRequests({ sessionId }: { sessionId: string | null | undefined }) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false) && !!sessionId;
  const { data } = useQuery({ ...chatAppRequestsOptions(wsId, sessionId ?? ""), enabled });
  return <AppRequestList requests={data ?? []} />;
}

/** One request, looked up from its chat or the caller's pending list (Inbox). */
export function InboxAppRequest({ requestId, chatSessionId }: { requestId: string; chatSessionId?: string | null }) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const { data } = useQuery({
    queryKey: [...chatAppRequestsOptions(wsId, chatSessionId ?? "").queryKey, "inbox", requestId],
    queryFn: () =>
      api.listAppRequests(chatSessionId ? { chatSessionId } : {}).then((r) => r.requests),
    enabled,
  });
  const request = data?.find((r) => r.id === requestId);
  if (!request) return null;
  return <AppRequestList requests={[request]} showDecided />;
}

function AppRequestList({ requests, showDecided = false }: { requests: AppRequest[]; showDecided?: boolean }) {
  const { t } = useT("settings");
  const viewerId = useAuthStore((s) => s.user?.id);
  const decide = useDecideAppRequest();
  const visible = showDecided ? requests : requests.filter((r) => r.status === "pending");
  if (visible.length === 0) return null;

  const batch = visible.filter(
    (r) => r.status === "pending" && r.kind === "action" && r.approver_user_id === viewerId,
  );
  const approveAll = async () => {
    let failed = 0;
    for (const r of batch) {
      try {
        await decide.mutateAsync({ id: r.id, decision: "approve" });
      } catch {
        failed++;
      }
    }
    if (failed) toast.error(t(($) => $.connected_apps.request.toast_failed));
    else toast.success(t(($) => $.connected_apps.request.toast_approved));
  };

  return (
    <section className="my-3 space-y-2" aria-label={t(($) => $.connected_apps.request.pending_count, { count: visible.length })}>
      {batch.length > 1 && (
        <div className="flex items-center justify-between gap-3 text-caption text-muted-foreground">
          <span>{t(($) => $.connected_apps.request.pending_count, { count: batch.length })}</span>
          <Button size="xs" variant="outline" disabled={decide.isPending} onClick={approveAll}>
            {t(($) => $.connected_apps.request.approve_all)}
          </Button>
        </div>
      )}
      {visible.map((request) => (
        <AppRequestCard key={request.id} request={request} isApprover={request.approver_user_id === viewerId} />
      ))}
    </section>
  );
}

function prettyArgs(args: Record<string, unknown> | undefined) {
  return JSON.stringify(args ?? {}, null, 2);
}

function AppRequestCard({ request, isApprover }: { request: AppRequest; isApprover: boolean }) {
  const { t } = useT("settings");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const decide = useDecideAppRequest();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(() => prettyArgs(request.arguments));
  const [rejecting, setRejecting] = useState(false);
  const [reason, setReason] = useState("");
  const [connecting, setConnecting] = useState(false);

  const agent = getActorName("agent", request.agent_id);
  const approver = getActorName("member", request.approver_user_id);
  const app = request.toolkit_slug;
  const pending = request.status === "pending";

  const approve = () => {
    let args: Record<string, unknown> | undefined;
    if (editing) {
      try {
        const parsed: unknown = JSON.parse(draft);
        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error();
        args = parsed as Record<string, unknown>;
      } catch {
        toast.error(t(($) => $.connected_apps.request.invalid_json));
        return;
      }
    }
    decide.mutate(
      { id: request.id, decision: "approve", arguments: args },
      {
        onSuccess: () => toast.success(t(($) => $.connected_apps.request.toast_approved)),
        onError: (e) =>
          toast.error(
            e instanceof Error && /connect the account/i.test(e.message)
              ? t(($) => $.connected_apps.request.not_connected)
              : t(($) => $.connected_apps.request.toast_failed),
          ),
      },
    );
  };
  const reject = () =>
    decide.mutate(
      { id: request.id, decision: "reject", reason: reason.trim() || undefined },
      {
        onSuccess: () => toast.success(t(($) => $.connected_apps.request.toast_rejected)),
        onError: () => toast.error(t(($) => $.connected_apps.request.toast_failed)),
      },
    );
  const connect = async () => {
    setConnecting(true);
    try {
      const { redirect_url } = await api.connectApp("personal", app);
      window.location.href = redirect_url;
    } catch {
      toast.error(t(($) => $.connected_apps.toast_failed));
      setConnecting(false);
    }
  };

  const statusLabel = (status: string) => {
    switch (status) {
      case "approved":
      case "rejected":
      case "failed":
      case "expired":
      case "cancelled":
        return t(($) => $.connected_apps.request.status[status]);
      default:
        return status;
    }
  };

  return (
    <div className="rounded-lg border border-brand/40 bg-brand/5 p-3">
      <div className="flex items-start gap-2">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-brand" />
        <div className="min-w-0 flex-1 space-y-1">
          <p className="text-body font-medium">
            {request.kind === "connect"
              ? t(($) => $.connected_apps.request.title_connect, { agent, app })
              : t(($) => $.connected_apps.request.title_action, { agent, tool: request.tool_slug })}
          </p>
          <p className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
            <Badge variant="outline">{t(($) => $.connected_apps.scopes[request.account_scope])}</Badge>
            {request.kind === "action" && <Badge variant="outline">{request.action_class}</Badge>}
            <span>
              {pending
                ? `${t(($) => $.connected_apps.request.waiting_for, { name: approver })} · ${t(($) => $.connected_apps.request.expires, { when: new Date(request.expires_at).toLocaleString(locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) })}`
                : statusLabel(request.status)}
            </span>
          </p>
          {request.summary && <p className="text-body">{request.summary}</p>}
          {request.kind === "action" &&
            (request.redacted ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.connected_apps.request.redacted, { name: approver })}
              </p>
            ) : editing ? (
              <Textarea
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                className="min-h-32 font-mono text-caption"
                aria-label={t(($) => $.connected_apps.request.arguments)}
              />
            ) : (
              <pre className="max-h-48 overflow-auto rounded-md bg-muted p-2 font-mono text-caption">
                {prettyArgs(request.approved_arguments ?? request.arguments)}
              </pre>
            ))}
          {request.error && <p className="text-caption text-destructive">{request.error}</p>}
        </div>
      </div>

      {pending && isApprover && (
        <div className="mt-3 space-y-2">
          {rejecting && (
            <Input
              autoFocus
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder={t(($) => $.connected_apps.request.reject_reason)}
            />
          )}
          <div className="flex flex-wrap items-center justify-end gap-2">
            {request.kind === "action" && !editing && !rejecting && (
              <Button size="xs" variant="ghost" onClick={() => setEditing(true)}>
                <Pencil />
                {t(($) => $.connected_apps.request.edit)}
              </Button>
            )}
            <Button
              size="xs"
              variant="outline"
              disabled={decide.isPending}
              onClick={() => (rejecting ? reject() : setRejecting(true))}
            >
              {t(($) => $.connected_apps.request.reject)}
            </Button>
            {request.kind === "connect" ? (
              <>
                <Button size="xs" variant="outline" disabled={connecting} onClick={connect}>
                  {connecting ? <Loader2 className="animate-spin" /> : <Plug />}
                  {t(($) => $.connected_apps.request.connect, { app })}
                </Button>
                <Button size="xs" disabled={decide.isPending} onClick={approve}>
                  {t(($) => $.connected_apps.request.connected_continue)}
                </Button>
              </>
            ) : (
              !rejecting && (
                <Button size="xs" disabled={decide.isPending} onClick={approve}>
                  {decide.isPending && <Loader2 className="animate-spin" />}
                  {t(($) => $.connected_apps.request.approve)}
                </Button>
              )
            )}
          </div>
        </div>
      )}
    </div>
  );
}
