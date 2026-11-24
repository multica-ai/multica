"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Building2, Loader2, Pencil, Plug, ShieldCheck } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { ApiError, api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import { FEATURE_LOCKED_CODE, workspaceFeatureKeys } from "@multica/core/billing";
import {
  appCatalogOptions,
  appsOverviewOptions,
  chatAppRequestsOptions,
  connectedAppsKeys,
  issueAppRequestsOptions,
  useDecideAppRequest,
  useDismissTeamAppRequests,
} from "@multica/core/connected-apps";
import { useActorName } from "@multica/core/workspace/hooks";
import type { AppRequest } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { useLocale, useT } from "../i18n";
import { AppLink, useNavigation } from "../navigation";
import { appAccountStatus } from "./app-status";
import { ConnectAppDialog } from "./connect-app-dialog";

const CONNECTED_PARAM = "app_connected";
const CONNECT_ERROR_PARAM = "app_connect_error";
// Set next to the error when the plan refused the account; older servers omit it.
const CONNECT_ERROR_REASON_PARAM = "app_connect_error_reason";
let lastConnectReturn = "";

/**
 * Connecting from a card leaves the app and comes back to the page it started
 * on with `?app_connected=<slug>` (or `app_connect_error`). The server has
 * already let the waiting agent continue; say so once, then drop the param so
 * a refresh does not repeat it.
 */
function useConnectReturn() {
  const { t } = useT("settings");
  const { t: tBilling } = useT("billing");
  const navigation = useNavigation();
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const connected = navigation.searchParams?.get(CONNECTED_PARAM) ?? null;
  const failed = navigation.searchParams?.get(CONNECT_ERROR_PARAM) ?? null;
  const failedReason = navigation.searchParams?.get(CONNECT_ERROR_REASON_PARAM) ?? null;
  useEffect(() => {
    const app = connected ?? failed;
    if (!app) return;
    // Several cards can mount on one page; only the first reports.
    const key = `${navigation.pathname}:${connected ? "ok" : "error"}:${app}`;
    if (lastConnectReturn === key) return;
    lastConnectReturn = key;
    if (connected) {
      toast.success(t(($) => $.connected_apps.request.connected_resumed, { app }));
      void qc.invalidateQueries({ queryKey: connectedAppsKeys.all(wsId) });
    } else if (failedReason === FEATURE_LOCKED_CODE) {
      toast.error(tBilling(($) => $.paid_feature.locked_error));
      void qc.invalidateQueries({ queryKey: workspaceFeatureKeys.all(wsId) });
    } else {
      toast.error(t(($) => $.connected_apps.toast_connect_failed));
    }
    const params = new URLSearchParams(navigation.searchParams);
    params.delete(CONNECTED_PARAM);
    params.delete(CONNECT_ERROR_PARAM);
    params.delete(CONNECT_ERROR_REASON_PARAM);
    const search = params.toString();
    navigation.replace(`${navigation.pathname}${search ? `?${search}` : ""}${navigation.hash}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, failed]);
}

/** Pending app confirmations of an issue, shown above its composer. The Inbox
 * passes the request it opened so that card scrolls into view; bumping
 * `highlightToken` replays it. */
export function IssueAppRequests({
  issueId,
  highlightId,
  highlightToken,
}: {
  issueId: string;
  highlightId?: string;
  highlightToken?: number;
}) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const { data } = useQuery({ ...issueAppRequestsOptions(wsId, issueId), enabled });
  useConnectReturn();
  return <AppRequestList requests={data ?? []} highlightId={highlightId} highlightToken={highlightToken} />;
}

/** Pending app confirmations of a chat, shown above its input. */
export function ChatAppRequests({ sessionId }: { sessionId: string | null | undefined }) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false) && !!sessionId;
  const { data } = useQuery({ ...chatAppRequestsOptions(wsId, sessionId ?? ""), enabled });
  useConnectReturn();
  return <AppRequestList requests={data ?? []} />;
}

/** One request, looked up from its chat or the caller's pending list (Inbox). */
export function InboxAppRequest({ requestId, chatSessionId }: { requestId: string; chatSessionId?: string | null }) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  useConnectReturn();
  const { data } = useQuery({
    queryKey: [...chatAppRequestsOptions(wsId, chatSessionId ?? "").queryKey, "inbox", requestId],
    queryFn: () =>
      api.listAppRequests(chatSessionId ? { chatSessionId } : {}).then((r) => r.requests),
    enabled,
  });
  const request = data?.find((r) => r.id === requestId);
  if (!request) return null;
  if (isTeamConnect(request) && request.can_decide && request.status === "pending") {
    const waiting = (data ?? []).filter(
      (r) => isTeamConnect(r) && r.status === "pending" && r.toolkit_slug === request.toolkit_slug,
    );
    return <TeamAccountRequests slug={request.toolkit_slug} requests={waiting} />;
  }
  return <AppRequestList requests={[request]} showDecided />;
}

function isTeamConnect(request: AppRequest) {
  return request.kind === "connect" && request.account_scope === "team";
}

/**
 * Everything waiting for one team account, as an admin sees it in the Inbox:
 * agents' runs and members' asks are one kind of request, so connecting the
 * account once answers them all.
 */
function TeamAccountRequests({ slug, requests }: { slug: string; requests: AppRequest[] }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { getActorName } = useActorName();
  const catalog = useQuery(appCatalogOptions(wsId));
  const decline = useDismissTeamAppRequests();
  const [connectOpen, setConnectOpen] = useState(false);
  const entry = catalog.data?.apps.find((a) => a.slug === slug);
  const app = entry?.name || slug;
  const declineAll = () =>
    decline.mutateAsync(slug).then(
      () => toast.success(t(($) => $.connected_apps.account_row.toast_declined)),
      () => toast.error(t(($) => $.connected_apps.request.toast_failed)),
    );

  return (
    <section className="my-3 space-y-2 rounded-lg border border-brand/40 bg-brand/5 p-3" data-team-account-requests={slug}>
      <div className="flex items-start gap-2">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-brand" />
        <div className="min-w-0 flex-1 space-y-1">
          <p className="text-body font-medium">{t(($) => $.connected_apps.team_requests.title, { app })}</p>
          <p className="flex items-center gap-1 text-caption text-muted-foreground">
            <Building2 className="size-3" aria-hidden="true" />
            {t(($) => $.connected_apps.owner.team)}
          </p>
        </div>
      </div>
      <ul className="divide-y rounded-md border bg-background">
        {requests.map((r) => {
          const run = !!r.agent_id && !!(r.issue_id || r.chat_session_id);
          return (
            <li key={r.id} className="flex items-center gap-2 px-3 py-2 text-body">
              <span className="min-w-0 flex-1 truncate">
                <span className="font-medium">
                  {run ? getActorName("agent", r.agent_id) : getActorName("member", r.approver_user_id)}
                </span>
                {r.summary ? (
                  <span className="text-muted-foreground">
                    {t(($) => $.connected_apps.team_requests.summary, { summary: r.summary })}
                  </span>
                ) : null}
              </span>
              <Badge variant="outline" className="shrink-0">
                {run ? t(($) => $.connected_apps.team_requests.run_waiting) : t(($) => $.connected_apps.team_requests.asked)}
              </Badge>
            </li>
          );
        })}
      </ul>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.connected_apps.team_requests.resumes)}
      </p>
      <div className="flex flex-wrap items-center justify-end gap-2">
        <Button size="xs" variant="ghost" nativeButton={false} render={<AppLink href={paths.appDetail(slug)} />}>
          {t(($) => $.connected_apps.team_requests.open_app)}
        </Button>
        <Button size="xs" variant="ghost" disabled={decline.isPending} onClick={declineAll}>
          {t(($) => $.connected_apps.team_requests.decline_all)}
        </Button>
        <Button size="xs" onClick={() => setConnectOpen(true)}>
          <Plug />
          {t(($) => $.connected_apps.request.connect_team)}
        </Button>
      </div>
      <ConnectAppDialog
        open={connectOpen}
        onOpenChange={setConnectOpen}
        scope="team"
        slug={slug}
        name={app}
        logo={entry?.logo}
      />
    </section>
  );
}

function AppRequestList({
  requests,
  showDecided = false,
  highlightId,
  highlightToken,
}: {
  requests: AppRequest[];
  showDecided?: boolean;
  highlightId?: string;
  highlightToken?: number;
}) {
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
    let limited = 0;
    for (const r of batch) {
      try {
        await decide.mutateAsync({ id: r.id, decision: "approve" });
      } catch (e) {
        if (e instanceof ApiError && e.status === 429) limited++;
        else failed++;
      }
    }
    if (limited) toast.error(t(($) => $.connected_apps.request.rate_limited_some, { count: limited }));
    else if (failed) toast.error(t(($) => $.connected_apps.request.toast_failed));
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
        <AppRequestCard
          key={request.id}
          request={request}
          isApprover={request.approver_user_id === viewerId}
          highlightToken={request.id === highlightId ? (highlightToken ?? 0) : undefined}
        />
      ))}
    </section>
  );
}

type SettingsT = ReturnType<typeof useT<"settings">>["t"];

function approveErrorMessage(e: unknown, t: SettingsT): string {
  if (e instanceof ApiError) {
    // 429 leaves the request pending; approving again later runs it.
    if (e.status === 429) return t(($) => $.connected_apps.request.rate_limited);
    if (e.status === 403) return t(($) => $.connected_apps.request.withdrawn);
  }
  if (e instanceof Error && /connect the account/i.test(e.message)) {
    return t(($) => $.connected_apps.request.not_connected);
  }
  return t(($) => $.connected_apps.request.toast_failed);
}

function prettyArgs(args: Record<string, unknown> | undefined) {
  return JSON.stringify(args ?? {}, null, 2);
}

const HIGHLIGHT_MS = 2400;

function AppRequestCard({
  request,
  isApprover,
  highlightToken,
}: {
  request: AppRequest;
  isApprover: boolean;
  highlightToken?: number;
}) {
  const { t } = useT("settings");
  const { t: ta } = useT("agents");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const ref = useRef<HTMLDivElement>(null);
  const [highlighted, setHighlighted] = useState(false);
  useEffect(() => {
    if (highlightToken === undefined) return;
    ref.current?.scrollIntoView({ block: "center", behavior: "smooth" });
    setHighlighted(true);
    const timer = setTimeout(() => setHighlighted(false), HIGHLIGHT_MS);
    return () => clearTimeout(timer);
  }, [highlightToken]);
  const { getActorName } = useActorName();
  const decide = useDecideAppRequest();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(() => prettyArgs(request.arguments));
  const [rejecting, setRejecting] = useState(false);
  const [reason, setReason] = useState("");
  const [connectOpen, setConnectOpen] = useState(false);

  const agent = getActorName("agent", request.agent_id);
  const approver = getActorName("member", request.approver_user_id);
  const pending = request.status === "pending";
  const isConnect = request.kind === "connect";
  const team = isTeamConnect(request);
  // Older servers omit can_decide; the approver answered everything then.
  const canDecide = request.can_decide ?? isApprover;
  const memberAsk = team && !request.agent_id;
  const addsApp = isConnect && request.adds_app === true;
  const catalog = useQuery({ ...appCatalogOptions(wsId), enabled: isConnect });
  const overview = useQuery({ ...appsOverviewOptions(wsId), enabled: addsApp && pending && isApprover });
  const entry = catalog.data?.apps.find((a) => a.slug === request.toolkit_slug);
  const app = entry?.name || request.toolkit_slug;
  const hasAccount = appAccountStatus(overview.data, "personal", request.toolkit_slug) === "connected";

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
        onError: (e) => toast.error(approveErrorMessage(e, t)),
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
  const withdraw = () =>
    decide.mutate(
      { id: request.id, decision: "reject" },
      {
        onSuccess: () => toast.success(t(($) => $.connected_apps.account_row.toast_withdrawn)),
        onError: () => toast.error(t(($) => $.connected_apps.request.toast_failed)),
      },
    );
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
    <div
      ref={ref}
      data-app-request={request.id}
      className={cn(
        "rounded-lg border border-brand/40 bg-brand/5 p-3 transition-shadow duration-300",
        highlighted && "ring-2 ring-brand/60",
      )}
    >
      <div className="flex items-start gap-2">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-brand" />
        <div className="min-w-0 flex-1 space-y-1">
          <p className="text-body font-medium">
            {addsApp
              ? ta(($) => $.app_requests.title_adds_app, { agent, app })
              : memberAsk
                ? t(($) => $.connected_apps.request.title_member_ask, { name: approver, app })
                : team
                  ? t(($) => $.connected_apps.request.title_connect_team, { agent, app })
                  : isConnect
                    ? t(($) => $.connected_apps.request.title_connect, { agent, app })
                    : t(($) => $.connected_apps.request.title_action, { agent, tool: request.tool_slug })}
          </p>
          <p className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
            <Badge variant="outline">
              {addsApp
                ? ta(($) => $.app_requests.new_app)
                : request.account_scope === "personal" && isApprover
                  ? t(($) => $.connected_apps.owner.personal)
                  : t(($) => $.connected_apps.scopes[request.account_scope])}
            </Badge>
            {request.kind === "action" && <Badge variant="outline">{request.action_class}</Badge>}
            <span>
              {pending
                ? `${team ? t(($) => $.connected_apps.request.waiting_for_admin) : t(($) => $.connected_apps.request.waiting_for, { name: approver })} · ${t(($) => $.connected_apps.request.expires, { when: new Date(request.expires_at).toLocaleString(locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) })}`
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

      {pending && team && (
        <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
          <p className="mr-auto text-caption text-muted-foreground">
            {canDecide
              ? t(($) => $.connected_apps.request.team_note)
              : t(($) => $.connected_apps.request.admins_notified)}
          </p>
          {canDecide ? (
            <>
              <Button size="xs" variant="ghost" disabled={decide.isPending} onClick={reject}>
                {t(($) => $.connected_apps.request.reject)}
              </Button>
              <Button size="xs" onClick={() => setConnectOpen(true)}>
                <Plug />
                {t(($) => $.connected_apps.request.connect_team)}
              </Button>
            </>
          ) : isApprover ? (
            <Button size="xs" variant="ghost" disabled={decide.isPending} onClick={withdraw}>
              {t(($) => $.connected_apps.account_row.withdraw)}
            </Button>
          ) : null}
        </div>
      )}
      {pending && !team && isApprover && (
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
            {addsApp && (
              <p className="mr-auto text-caption text-muted-foreground">
                {ta(($) => $.app_requests.adds_app_note, { app, agent })}
              </p>
            )}
            {request.kind === "action" && !editing && !rejecting && (
              <Button size="xs" variant="ghost" onClick={() => setEditing(true)}>
                <Pencil />
                {t(($) => $.connected_apps.request.edit)}
              </Button>
            )}
            <Button
              size="xs"
              variant={addsApp ? "ghost" : "outline"}
              disabled={decide.isPending}
              onClick={() => (rejecting ? reject() : setRejecting(true))}
            >
              {t(($) => $.connected_apps.request.reject)}
            </Button>
            {addsApp ? (
              hasAccount ? (
                <Button size="xs" disabled={decide.isPending} onClick={approve}>
                  {decide.isPending && <Loader2 className="animate-spin" />}
                  {ta(($) => $.app_requests.add_to_agent, { app, agent })}
                </Button>
              ) : (
                <Button size="xs" onClick={() => setConnectOpen(true)}>
                  <Plug />
                  {t(($) => $.connected_apps.request.connect, { app })}
                </Button>
              )
            ) : isConnect ? (
              <>
                <Button size="xs" variant="outline" onClick={() => setConnectOpen(true)}>
                  <Plug />
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
      {isConnect && (
        <ConnectAppDialog
          open={connectOpen}
          onOpenChange={setConnectOpen}
          scope={team ? "team" : "personal"}
          slug={request.toolkit_slug}
          name={app}
          logo={entry?.logo}
        />
      )}
    </div>
  );
}
