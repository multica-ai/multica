"use client";

import { useEffect, useMemo, useState, useCallback } from "react";
import { Key, Trash2, Copy, Check, Plug, Sparkles, Code2 } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import type { PersonalAccessToken } from "@multica/core/types";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@multica/ui/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

const EXPIRY_KEYS = ["30", "90", "365", "never"] as const;

export function TokensTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  // The web app builds with an empty apiBaseUrl because Next.js rewrites
  // proxy /api/* to the backend on the same origin. For the connection-
  // details panel we want to show users something real to paste into env
  // vars, so we fall back to the page's own origin: requests to that host
  // still hit the same Next.js rewrite path, so MULTICA_SERVER_URL=<this>
  // works for the Multica CLI, the in-binary MCP server, or any other
  // client. Memoised because window.location.origin is stable per page
  // load and we don't want to retrigger child renders.
  const apiBaseUrl = useMemo(() => {
    let fromClient = "";
    try {
      fromClient = api.getBaseUrl() ?? "";
    } catch {
      fromClient = "";
    }
    if (fromClient) return fromClient;
    if (typeof window !== "undefined" && window.location?.origin) {
      return window.location.origin;
    }
    return "";
  }, []);

  const [tokens, setTokens] = useState<PersonalAccessToken[]>([]);
  const [tokenName, setTokenName] = useState("");
  const [tokenExpiry, setTokenExpiry] = useState("90");
  const [tokenCreating, setTokenCreating] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);
  const [tokenCopied, setTokenCopied] = useState(false);
  const [tokenRevoking, setTokenRevoking] = useState<string | null>(null);
  const [revokeConfirmId, setRevokeConfirmId] = useState<string | null>(null);
  const [tokensLoading, setTokensLoading] = useState(true);

  const loadTokens = useCallback(async () => {
    try {
      const list = await api.listPersonalAccessTokens();
      setTokens(list);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tokens.toast_load_failed));
    } finally {
      setTokensLoading(false);
    }
  }, [t]);

  useEffect(() => { loadTokens(); }, [loadTokens]);

  const handleCreateToken = async () => {
    setTokenCreating(true);
    try {
      const expiresInDays = tokenExpiry === "never" ? undefined : Number(tokenExpiry);
      const result = await api.createPersonalAccessToken({ name: tokenName, expires_in_days: expiresInDays });
      setNewToken(result.token);
      setTokenName("");
      setTokenExpiry("90");
      await loadTokens();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tokens.toast_create_failed));
    } finally {
      setTokenCreating(false);
    }
  };

  const handleRevokeToken = async (id: string) => {
    setTokenRevoking(id);
    try {
      await api.revokePersonalAccessToken(id);
      await loadTokens();
      toast.success(t(($) => $.tokens.toast_revoked));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tokens.toast_revoke_failed));
    } finally {
      setTokenRevoking(null);
    }
  };

  const handleCopyToken = async () => {
    if (!newToken) return;
    await navigator.clipboard.writeText(newToken);
    setTokenCopied(true);
    setTimeout(() => setTokenCopied(false), 2000);
  };

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Key className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">{t(($) => $.tokens.title)}</h2>
        </div>

        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              {t(($) => $.tokens.description)}
            </p>
            <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
              <Input
                type="text"
                value={tokenName}
                onChange={(e) => setTokenName(e.target.value)}
                placeholder={t(($) => $.tokens.name_placeholder)}
              />
              <Select value={tokenExpiry} onValueChange={(v) => { if (v) setTokenExpiry(v); }}>
                <SelectTrigger size="sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {EXPIRY_KEYS.map((key) => (
                    <SelectItem key={key} value={key}>{t(($) => $.tokens.expiry[key])}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button onClick={handleCreateToken} disabled={tokenCreating || !tokenName.trim()}>
                {tokenCreating ? t(($) => $.tokens.creating) : t(($) => $.tokens.create)}
              </Button>
            </div>
          </CardContent>
        </Card>

        {tokensLoading ? (
          <div className="space-y-2">
            {Array.from({ length: 2 }).map((_, i) => (
              <Card key={i}>
                <CardContent className="flex items-center gap-3">
                  <div className="flex-1 space-y-1.5">
                    <Skeleton className="h-4 w-32" />
                    <Skeleton className="h-3 w-48" />
                  </div>
                  <Skeleton className="h-8 w-8 rounded" />
                </CardContent>
              </Card>
            ))}
          </div>
        ) : tokens.length > 0 && (
          <div className="space-y-2">
            {tokens.map((token) => (
              <Card key={token.id}>
                <CardContent className="flex items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium truncate">{token.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {t(($) => $.tokens.metadata_prefix, {
                        prefix: token.token_prefix,
                        created: new Date(token.created_at).toLocaleDateString(),
                        lastUsed: token.last_used_at
                          ? t(($) => $.tokens.last_used_with_date, {
                              date: new Date(token.last_used_at!).toLocaleDateString(),
                            })
                          : t(($) => $.tokens.last_used_never),
                      })}
                      {token.expires_at && t(($) => $.tokens.expires_with_date, {
                        date: new Date(token.expires_at!).toLocaleDateString(),
                      })}
                    </div>
                  </div>
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setRevokeConfirmId(token.id)}
                          disabled={tokenRevoking === token.id}
                          aria-label={t(($) => $.tokens.revoke_aria, { name: token.name })}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      }
                    />
                    <TooltipContent>{t(($) => $.tokens.revoke_tooltip)}</TooltipContent>
                  </Tooltip>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </section>

      {/* ─── Connection details ────────────────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Plug className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">{t(($) => $.connection.title)}</h2>
        </div>
        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              {t(($) => $.connection.description)}
            </p>
            <CopyableField
              label={t(($) => $.connection.api_url_label)}
              value={apiBaseUrl}
              envVar="MULTICA_SERVER_URL"
              copyTooltip={t(($) => $.connection.copy_tooltip)}
              unavailableLabel={t(($) => $.connection.unavailable)}
            />
            <CopyableField
              label={t(($) => $.connection.workspace_id_label)}
              value={workspace?.id ?? ""}
              envVar="MULTICA_WORKSPACE_ID"
              copyTooltip={t(($) => $.connection.copy_tooltip)}
              unavailableLabel={t(($) => $.connection.unavailable)}
            />
            {workspace?.name ? (
              <CopyableField
                label={t(($) => $.connection.workspace_name_label)}
                value={workspace.name}
                copyTooltip={t(($) => $.connection.copy_tooltip)}
                unavailableLabel={t(($) => $.connection.unavailable)}
              />
            ) : null}
          </CardContent>
        </Card>
      </section>

      {/* ─── Model Context Protocol setup ──────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Sparkles className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">{t(($) => $.mcp.title)}</h2>
        </div>
        <Card>
          <CardContent className="space-y-5">
            <p className="text-xs text-muted-foreground">
              {t(($) => $.mcp.description)}
            </p>

            <Step n={1} title={t(($) => $.mcp.step_create_token_title)}>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.mcp.step_create_token_body)}
              </p>
            </Step>

            <Step n={2} title={t(($) => $.mcp.step_register_title)}>
              <ClientHeading>
                {t(($) => $.mcp.claude_code_heading)}
              </ClientHeading>
              <CodeBlock
                text={buildClaudeCodeCommand({
                  apiBaseUrl,
                  workspaceId: workspace?.id ?? "<workspace-id>",
                })}
                copyTooltip={t(($) => $.connection.copy_tooltip)}
              />
              <ClientHeading>
                {t(($) => $.mcp.claude_desktop_heading)}
              </ClientHeading>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.mcp.claude_desktop_description)}
              </p>
              <CodeBlock
                text={buildClaudeDesktopConfig({
                  apiBaseUrl,
                  workspaceId: workspace?.id ?? "<workspace-id>",
                })}
                copyTooltip={t(($) => $.connection.copy_tooltip)}
              />
              <ClientHeading>
                {t(($) => $.mcp.step_register_other_clients_heading)}
              </ClientHeading>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.mcp.step_register_other_clients_body)}
              </p>
            </Step>

            <Step n={3} title={t(($) => $.mcp.step_try_title)}>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.mcp.step_try_body)}
              </p>
            </Step>

            <p className="rounded-md border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
              {t(($) => $.mcp.security_note)}
            </p>
          </CardContent>
        </Card>
      </section>

      {/* ─── REST API reference ────────────────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Code2 className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">{t(($) => $.rest.title)}</h2>
        </div>
        <Card>
          <CardContent className="space-y-5 text-sm">
            <p className="text-xs text-muted-foreground">
              {t(($) => $.rest.intro)}
            </p>

            <div className="space-y-2">
              <h3 className="text-xs font-medium">
                {t(($) => $.rest.auth_heading)}
              </h3>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.rest.auth_body_prefix)}
                <CodeChip text="Bearer" />
                {t(($) => $.rest.auth_body_bearer_suffix)}
                <CodeChip text="X-Workspace-ID" />
                {t(($) => $.rest.auth_body_workspace_suffix)}
                <CodeChip text="application/json" />
                {t(($) => $.rest.auth_body_content_type_suffix)}
              </p>
              <CodeBlock
                text={buildCurlSample({
                  apiBaseUrl,
                  workspaceId: workspace?.id ?? "<workspace-id>",
                })}
                copyTooltip={t(($) => $.connection.copy_tooltip)}
              />
            </div>

            <div className="space-y-2">
              <h3 className="text-xs font-medium">
                {t(($) => $.rest.endpoints_heading)}
              </h3>
              <p className="text-xs text-muted-foreground">
                {t(($) => $.rest.endpoints_body_prefix)}
                <CodeChip text="{like-this}" />
                {t(($) => $.rest.endpoints_body_suffix)}
              </p>

              {/* Resource-group + endpoint-row data is intentionally
                  defined as JSX rather than data-driven. Each row is a
                  hand-curated description matching the actual server
                  handler — the value is in the prose, so generating it
                  from a table would lose the per-row notes. Group titles
                  stay English (proper nouns matching API path segments). */}
              <EndpointGroup title="Issues">
                <EndpointRow method="GET" path="/api/issues" desc="List issues. Query: status, priority, assignee, project, limit, offset." />
                <EndpointRow method="GET" path="/api/issues/search" desc="Full-text search. Query: q, limit." />
                <EndpointRow method="POST" path="/api/issues" desc="Create. Body: { title, description?, status?, priority?, assignee_type?, assignee_id?, parent_issue_id?, project_id?, due_date? }." />
                <EndpointRow method="GET" path="/api/issues/{id}" desc="Get one. id may be a UUID or human identifier (e.g. MUL-123)." />
                <EndpointRow method="PUT" path="/api/issues/{id}" desc="Update. Body fields are all optional; only what's passed is changed." />
                <EndpointRow method="DELETE" path="/api/issues/{id}" desc="Delete." />
                <EndpointRow method="GET / POST" path="/api/issues/{id}/comments" desc="List or create a comment. Create body: { content, parent_id? }." />
                <EndpointRow method="GET" path="/api/issues/{id}/task-runs" desc="List task runs (status, dispatched_at, completed_at, error)." />
                <EndpointRow method="GET / POST" path="/api/issues/{id}/labels" desc="List labels on an issue, or attach one. Attach body: { label_id }." />
                <EndpointRow method="DELETE" path="/api/issues/{id}/labels/{labelId}" desc="Detach a label." />
                <EndpointRow method="GET / POST" path="/api/issues/{id}/subscribers" desc="List subscribers, or subscribe. Body: { actor_type, actor_id }." />
                <EndpointRow method="POST" path="/api/issues/{id}/reactions" desc="Add a reaction. Body: { emoji }." />
                <EndpointRow method="POST" path="/api/issues/quick-create" desc="One-shot natural-language create that dispatches an agent task to fill in the rest." />
              </EndpointGroup>

              <EndpointGroup title="Channels">
                <EndpointRow method="GET" path="/api/channels" desc="List channels and DMs the actor belongs to (with per-channel unread counts)." />
                <EndpointRow method="POST" path="/api/channels" desc="Create channel. Body: { name, display_name?, description?, visibility?, retention_days? }." />
                <EndpointRow method="GET" path="/api/channels/search" desc="Full-text search messages. Query: q, limit." />
                <EndpointRow method="GET / PATCH / DELETE" path="/api/channels/{channelId}" desc="Get / update / archive a channel." />
                <EndpointRow method="POST" path="/api/channels/{channelId}/read" desc="Update read cursor. Body: { message_id }." />
                <EndpointRow method="GET / POST" path="/api/channels/{channelId}/members" desc="List members, or add one. Body: { member_type, member_id, role? }." />
                <EndpointRow method="DELETE" path="/api/channels/{channelId}/members/{memberType}/{memberId}" desc="Remove a member." />
                <EndpointRow method="GET / POST" path="/api/channels/{channelId}/messages" desc="List or post messages. List query: limit, before (RFC3339), include_threaded. Post body: { content, parent_message_id?, attachment_ids? }." />
                <EndpointRow method="PATCH / DELETE" path="/api/channels/{channelId}/messages/{messageId}" desc="Edit (author-only) or soft-delete a message." />
                <EndpointRow method="GET" path="/api/channels/{channelId}/messages/{messageId}/thread" desc="List replies under a message." />
                <EndpointRow method="POST / DELETE" path="/api/channels/{channelId}/messages/{messageId}/reactions" desc="Add or remove a reaction. Body: { emoji }." />
                <EndpointRow method="POST" path="/api/dms" desc="Get or create a DM. Body: { participants: [{type, id}, …] }." />
              </EndpointGroup>

              <EndpointGroup title="Agents">
                <EndpointRow method="GET" path="/api/agents" desc="List agents. Query: include_archived." />
                <EndpointRow method="POST" path="/api/agents" desc="Create an agent." />
                <EndpointRow method="GET / PUT" path="/api/agents/{id}" desc="Get / update an agent." />
                <EndpointRow method="POST" path="/api/agents/{id}/archive" desc="Archive (soft-disable)." />
                <EndpointRow method="POST" path="/api/agents/{id}/restore" desc="Un-archive." />
                <EndpointRow method="GET" path="/api/agents/{id}/tasks" desc="List the agent's recent tasks. Query: limit." />
                <EndpointRow method="POST" path="/api/agents/{id}/cancel-tasks" desc="Cancel all in-flight tasks for this agent." />
                <EndpointRow method="GET / PUT" path="/api/agents/{id}/skills" desc="List or replace the agent's attached skills." />
              </EndpointGroup>

              <EndpointGroup title="Projects">
                <EndpointRow method="GET / POST" path="/api/projects" desc="List or create projects." />
                <EndpointRow method="GET" path="/api/projects/search" desc="Fuzzy search by name. Query: q." />
                <EndpointRow method="GET / PUT / DELETE" path="/api/projects/{id}" desc="Get / update / delete a project." />
                <EndpointRow method="GET / POST" path="/api/projects/{id}/resources" desc="List or attach resources (URLs, docs)." />
              </EndpointGroup>

              <EndpointGroup title="Memory">
                <EndpointRow method="GET / POST" path="/api/memory" desc="List or create memory artifacts (wiki / agent_note / runbook / decision)." />
                <EndpointRow method="GET" path="/api/memory/search" desc="Full-text search across artifacts. Query: q, kind, limit, offset." />
                <EndpointRow method="GET" path="/api/memory/by-anchor/{type}/{id}" desc="List artifacts anchored to a specific issue / project / agent / channel." />
                <EndpointRow method="GET / PUT" path="/api/memory/{id}" desc="Get / update a memory artifact." />
                <EndpointRow method="POST" path="/api/memory/{id}/archive" desc="Soft-delete; reversible via /restore." />
                <EndpointRow method="POST" path="/api/memory/{id}/restore" desc="Un-archive." />
              </EndpointGroup>

              <EndpointGroup title="Labels">
                <EndpointRow method="GET / POST" path="/api/labels" desc="List or create labels." />
                <EndpointRow method="GET / PUT / DELETE" path="/api/labels/{id}" desc="Get / update / delete a label." />
              </EndpointGroup>

              <EndpointGroup title="Autopilots">
                <EndpointRow method="GET / POST" path="/api/autopilots" desc="List or create autopilots." />
                <EndpointRow method="GET / PATCH / DELETE" path="/api/autopilots/{id}" desc="Get / update / delete an autopilot." />
                <EndpointRow method="POST" path="/api/autopilots/{id}/trigger" desc="Manually start a run. Body: { payload? }." />
                <EndpointRow method="GET" path="/api/autopilots/{id}/runs" desc="List execution history. Query: limit." />
                <EndpointRow method="GET / POST" path="/api/autopilots/{id}/triggers" desc="List or create scheduled triggers." />
              </EndpointGroup>

              <EndpointGroup title="Workspace & Members">
                <EndpointRow method="GET" path="/api/workspaces" desc="List the workspaces the caller belongs to." />
                <EndpointRow method="POST" path="/api/workspaces" desc="Create a new workspace." />
                <EndpointRow method="GET" path="/api/workspaces/{id}" desc="Get workspace details (settings, feature flags, retention)." />
                <EndpointRow method="PATCH / PUT" path="/api/workspaces/{id}" desc="Update workspace fields (admin/owner only)." />
                <EndpointRow method="GET" path="/api/workspaces/{id}/members" desc="List members with their user records." />
                <EndpointRow method="POST" path="/api/workspaces/{id}/members" desc="Invite a member by email (admin/owner only)." />
                <EndpointRow method="PATCH / DELETE" path="/api/workspaces/{id}/members/{memberId}" desc="Update role / remove member (admin/owner only)." />
              </EndpointGroup>

              <EndpointGroup title="Account & tokens">
                <EndpointRow method="GET / PATCH" path="/api/me" desc="Read or update the current user's profile." />
                <EndpointRow method="GET / POST" path="/api/tokens" desc="List or create personal access tokens." />
                <EndpointRow method="DELETE" path="/api/tokens/{id}" desc="Revoke a token." />
                <EndpointRow method="GET" path="/api/inbox" desc="The current user's inbox (mentions, assignments)." />
                <EndpointRow method="GET" path="/api/notification-preferences" desc="Per-user notification toggles." />
              </EndpointGroup>
            </div>

            <div className="space-y-2 rounded-md border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
              <p>
                <strong className="text-foreground">
                  {t(($) => $.rest.conventions_label)}
                </strong>
                {t(($) => $.rest.conventions_body_prefix)}
                <CodeChip text="limit" />
                {t(($) => $.rest.conventions_body_limit_suffix)}
                <CodeChip text="offset" />
                {t(($) => $.rest.conventions_body_offset_suffix)}
                <CodeChip text="before" />
                {t(($) => $.rest.conventions_body_before_suffix)}
                <CodeChip text={`{ error: "…" }`} />
                {t(($) => $.rest.conventions_body_error_suffix)}
              </p>
              <p>
                <strong className="text-foreground">
                  {t(($) => $.rest.no_openapi_label)}
                </strong>
                {t(($) => $.rest.no_openapi_body_prefix)}
                <CodeChip text="server/cmd/server/router.go" />
                {t(($) => $.rest.no_openapi_body_suffix)}
              </p>
            </div>
          </CardContent>
        </Card>
      </section>

      <AlertDialog open={!!revokeConfirmId} onOpenChange={(v) => { if (!v) setRevokeConfirmId(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.tokens.revoke_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.tokens.revoke_dialog.description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.tokens.revoke_dialog.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={async () => {
                if (revokeConfirmId) await handleRevokeToken(revokeConfirmId);
                setRevokeConfirmId(null);
              }}
            >
              {t(($) => $.tokens.revoke_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={!!newToken} onOpenChange={(v) => { if (!v) { setNewToken(null); setTokenCopied(false); } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.tokens.created_dialog.title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.tokens.created_dialog.description)}
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <code className="flex-1 rounded-md border bg-muted/50 px-3 py-2 text-sm break-all select-all">
              {newToken}
            </code>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button variant="outline" size="icon" onClick={handleCopyToken}>
                    {tokenCopied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                  </Button>
                }
              />
              <TooltipContent>{t(($) => $.tokens.created_dialog.copy_tooltip)}</TooltipContent>
            </Tooltip>
          </div>
          <DialogFooter>
            <Button onClick={() => { setNewToken(null); setTokenCopied(false); }}>{t(($) => $.tokens.created_dialog.done)}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Connection-details / MCP-setup helpers
//
// These were a separate, longer wizard in the @multica/mcp PR (#1986) that
// crashed on render. The replacement is deliberately minimal: two display
// helpers (CopyableField, CodeBlock) + two pure builders that emit the
// command strings users paste into their AI client. No download flow, no
// multi-step rendering, no stateful sub-components beyond local copy-state.
// ---------------------------------------------------------------------------

function CopyableField({
  label,
  value,
  envVar,
  copyTooltip,
  unavailableLabel,
}: {
  label: string;
  value: string;
  envVar?: string;
  copyTooltip: string;
  unavailableLabel: string;
}) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    if (!value) return;
    await navigator.clipboard.writeText(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <div className="space-y-1">
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-xs font-medium">{label}</span>
        {envVar ? (
          <code className="text-[10px] uppercase tracking-wide text-muted-foreground">
            {envVar}
          </code>
        ) : null}
      </div>
      <div className="flex items-center gap-2">
        <code className="flex-1 truncate rounded-md border bg-muted/50 px-3 py-2 text-xs select-all">
          {value || (
            <span className="text-muted-foreground">{unavailableLabel}</span>
          )}
        </code>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="outline"
                size="icon-sm"
                onClick={copy}
                disabled={!value}
                aria-label={`${copyTooltip} ${label}`}
              >
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              </Button>
            }
          />
          <TooltipContent>{copyTooltip}</TooltipContent>
        </Tooltip>
      </div>
    </div>
  );
}

function CodeBlock({ text, copyTooltip }: { text: string; copyTooltip: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded-md border bg-muted/50 px-3 py-2 pr-10 text-xs leading-relaxed">
        <code>{text}</code>
      </pre>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={copy}
              className="absolute top-1.5 right-1.5"
              aria-label={copyTooltip}
            >
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            </Button>
          }
        />
        <TooltipContent>{copyTooltip}</TooltipContent>
      </Tooltip>
    </div>
  );
}

// `multica mcp-stdio` ships with the multica binary itself. This means the
// install command for users is just "make sure multica is on your PATH";
// no separate package, no chmod dance, no version skew. Builders below
// produce the snippet to paste into the user's AI-client config.

function buildClaudeCodeCommand({
  apiBaseUrl,
  workspaceId,
}: {
  apiBaseUrl: string;
  workspaceId: string;
}): string {
  const url = apiBaseUrl || "<api-base-url>";
  return [
    "claude mcp add --scope user multica \\",
    `  -e MULTICA_SERVER_URL=${url} \\`,
    "  -e MULTICA_TOKEN=mul_… \\",
    `  -e MULTICA_WORKSPACE_ID=${workspaceId} \\`,
    "  -- multica mcp-stdio",
  ].join("\n");
}

function buildClaudeDesktopConfig({
  apiBaseUrl,
  workspaceId,
}: {
  apiBaseUrl: string;
  workspaceId: string;
}): string {
  const url = apiBaseUrl || "<api-base-url>";
  return JSON.stringify(
    {
      mcpServers: {
        multica: {
          command: "multica",
          args: ["mcp-stdio"],
          env: {
            MULTICA_SERVER_URL: url,
            MULTICA_TOKEN: "mul_…",
            MULTICA_WORKSPACE_ID: workspaceId,
          },
        },
      },
    },
    null,
    2,
  );
}

// One-shot curl example for the REST API reference. Uses the workspace
// the user is currently looking at — they can copy + paste with one
// edit (their token).
function buildCurlSample({
  apiBaseUrl,
  workspaceId,
}: {
  apiBaseUrl: string;
  workspaceId: string;
}): string {
  const url = apiBaseUrl || "<api-base-url>";
  return [
    `curl ${url}/api/issues \\`,
    "  -H 'Authorization: Bearer mul_…' \\",
    `  -H 'X-Workspace-ID: ${workspaceId}'`,
  ].join("\n");
}

// ---------------------------------------------------------------------------
// Wizard / reference helper components
//
// Step — numbered step in the MCP setup wizard. Title is required, body
// is whatever React content. Numbers are passed in (not derived) so the
// caller controls ordering and can renumber if a step gets removed.
//
// ClientHeading — small uppercase subhead inside a Step body, used to
// label the per-client snippets ("Claude Code", "Claude Desktop", etc).
//
// EndpointGroup / EndpointRow / MethodBadge — REST-API reference rows.
// Group titles stay English (they're API path segments by convention),
// individual `desc` props are JSX attributes (i18next/no-literal-string
// runs in jsx-text-only mode here, so attribute strings pass through).
//
// CodeChip — inline `<code>` styled like Stripe/GitHub's API docs. Used
// for protocol-level identifiers in body text (Bearer, X-Workspace-ID,
// application/json) where translating the identifier itself would be
// wrong — they're literal strings the protocol requires.
// ---------------------------------------------------------------------------

function Step({
  n,
  title,
  children,
}: {
  n: number;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/15 text-[11px] font-semibold text-primary">
          {n}
        </span>
        <span className="text-sm font-medium">{title}</span>
      </div>
      <div className="ml-7 space-y-2">{children}</div>
    </div>
  );
}

function ClientHeading({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
      {children}
    </div>
  );
}

// `text` is a string prop (not children) so the literal sits in a JSX
// attribute. eslint-plugin-i18next runs in "jsx-text-only" mode here,
// which exempts attribute strings — we want it to: protocol-level
// identifiers like `Bearer`, `X-Workspace-ID`, or `application/json`
// must NOT be translated, they're literal wire values.
function CodeChip({ text }: { text: string }) {
  return (
    <code className="rounded bg-muted px-1 py-0.5 text-[11px]">{text}</code>
  );
}

function EndpointGroup({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5 pt-3 first:pt-0">
      <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        {title}
      </div>
      <div className="overflow-hidden rounded-md border border-border">
        <div className="divide-y divide-border">{children}</div>
      </div>
    </div>
  );
}

// `method` accepts a slash-joined string ("GET / POST") so a single row
// can describe multiple verbs supported on the same path. Each verb gets
// its own colored badge for scannability.
function EndpointRow({
  method,
  path,
  desc,
}: {
  method: string;
  path: string;
  desc: string;
}) {
  const methods = method
    .split("/")
    .map((m) => m.trim())
    .filter(Boolean);
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 gap-y-1 px-3 py-2 sm:grid-cols-[auto_minmax(0,2fr)_minmax(0,3fr)]">
      <div className="flex flex-wrap items-center gap-1">
        {methods.map((m) => (
          <MethodBadge key={m} method={m} />
        ))}
      </div>
      <code className="col-span-1 text-[11px] sm:text-xs break-all font-mono">
        {path}
      </code>
      <p className="col-span-2 text-[11px] text-muted-foreground sm:col-span-1">
        {desc}
      </p>
    </div>
  );
}

function MethodBadge({ method }: { method: string }) {
  const m = method.toUpperCase();
  // Coloring follows common API-doc conventions (Stripe, GitHub):
  // GET=blue, POST=green, PUT=amber, PATCH=violet, DELETE=red. Keeping
  // the mapping inline avoids adding a new shadcn variant for one use.
  const color =
    m === "GET"
      ? "bg-blue-500/15 text-blue-700 dark:text-blue-300"
      : m === "POST"
        ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300"
        : m === "PUT"
          ? "bg-amber-500/15 text-amber-700 dark:text-amber-300"
          : m === "PATCH"
            ? "bg-violet-500/15 text-violet-700 dark:text-violet-300"
            : m === "DELETE"
              ? "bg-red-500/15 text-red-700 dark:text-red-300"
              : "bg-muted text-foreground";
  return (
    <span
      className={`inline-flex min-w-[44px] items-center justify-center rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${color}`}
    >
      {m}
    </span>
  );
}
