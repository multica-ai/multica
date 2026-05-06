"use client";

import { useEffect, useMemo, useState, useCallback } from "react";
import { Key, Trash2, Copy, Check, Plug, Sparkles, ExternalLink, Code2 } from "lucide-react";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import type { PersonalAccessToken } from "@multica/core/types";
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
import { useCurrentWorkspace } from "@multica/core/paths";

/**
 * "API & MCP" settings tab. Exposes three things:
 *
 * 1. Personal access tokens — same list/create/revoke flow that lived in
 *    the old "API Tokens" tab.
 * 2. Connection details — the API base URL and current workspace ID,
 *    with one-click copy. These are the values users paste into env vars
 *    (CLI, MCP, custom integrations).
 * 3. MCP server setup — what the @multica/mcp server exposes plus
 *    copy-paste config snippets for Claude Code and Claude Desktop, with
 *    the workspace's actual API URL + workspace ID prefilled so users
 *    don't have to hand-edit placeholders.
 */
export function TokensTab() {
  const [tokens, setTokens] = useState<PersonalAccessToken[]>([]);
  const [tokenName, setTokenName] = useState("");
  const [tokenExpiry, setTokenExpiry] = useState("90");
  const [tokenCreating, setTokenCreating] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);
  const [tokenCopied, setTokenCopied] = useState(false);
  const [tokenRevoking, setTokenRevoking] = useState<string | null>(null);
  const [revokeConfirmId, setRevokeConfirmId] = useState<string | null>(null);
  const [tokensLoading, setTokensLoading] = useState(true);

  const workspace = useCurrentWorkspace();
  const apiBaseUrl = useMemo(() => {
    // The web app builds with an empty `apiBaseUrl` because Next.js
    // rewrites proxy /api/* to the backend on the same origin — no
    // absolute URL is needed at runtime. For the settings panel we DO
    // want to show users something real to paste into env vars, so we
    // fall back to the page's own origin: requests to that host will
    // hit the same Next.js rewrite path the in-app calls use, which
    // means `MULTICA_API_URL=<frontend-origin>` works for the MCP
    // server, the CLI, and any other client.
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

  const loadTokens = useCallback(async () => {
    try {
      const list = await api.listPersonalAccessTokens();
      setTokens(list);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to load tokens");
    } finally {
      setTokensLoading(false);
    }
  }, []);

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
      toast.error(e instanceof Error ? e.message : "Failed to create token");
    } finally {
      setTokenCreating(false);
    }
  };

  const handleRevokeToken = async (id: string) => {
    setTokenRevoking(id);
    try {
      await api.revokePersonalAccessToken(id);
      await loadTokens();
      toast.success("Token revoked");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to revoke token");
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
    <div className="space-y-10">
      {/* ─── Section 1: Tokens ─────────────────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Key className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Personal access tokens</h2>
        </div>

        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              One token works for everything: the{" "}
              <strong className="text-foreground">REST API</strong>, the{" "}
              <strong className="text-foreground">MCP server</strong>, and
              the <strong className="text-foreground">Multica CLI</strong>{" "}
              all use the same personal access token. Pair the token with
              the connection details below. Treat them like passwords —
              they&apos;re shown once on creation and can&apos;t be retrieved
              later.
            </p>
            <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
              <Input
                type="text"
                value={tokenName}
                onChange={(e) => setTokenName(e.target.value)}
                placeholder="Token name (e.g. MCP server)"
              />
              <Select value={tokenExpiry} onValueChange={(v) => { if (v) setTokenExpiry(v); }}>
                <SelectTrigger size="sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="30">30 days</SelectItem>
                  <SelectItem value="90">90 days</SelectItem>
                  <SelectItem value="365">1 year</SelectItem>
                  <SelectItem value="never">No expiry</SelectItem>
                </SelectContent>
              </Select>
              <Button onClick={handleCreateToken} disabled={tokenCreating || !tokenName.trim()}>
                {tokenCreating ? "Creating..." : "Create"}
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
            {tokens.map((t) => (
              <Card key={t.id}>
                <CardContent className="flex items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium truncate">{t.name}</div>
                    <div className="text-xs text-muted-foreground">
                      {t.token_prefix}... · Created {new Date(t.created_at).toLocaleDateString()} · {t.last_used_at ? `Last used ${new Date(t.last_used_at).toLocaleDateString()}` : "Never used"}
                      {t.expires_at && ` · Expires ${new Date(t.expires_at).toLocaleDateString()}`}
                    </div>
                  </div>
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setRevokeConfirmId(t.id)}
                          disabled={tokenRevoking === t.id}
                          aria-label={`Revoke ${t.name}`}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      }
                    />
                    <TooltipContent>Revoke</TooltipContent>
                  </Tooltip>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </section>

      {/* ─── Section 2: Connection details ─────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Plug className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Connection details</h2>
        </div>
        <Card>
          <CardContent className="space-y-4">
            <p className="text-xs text-muted-foreground">
              These are the values you&apos;ll plug into the CLI, the MCP
              server, or any other client. Pair the values below with a
              personal access token from the section above.
            </p>
            <CopyableField label="API base URL" value={apiBaseUrl || ""} envVar="MULTICA_API_URL" />
            <CopyableField label="Workspace ID" value={workspace?.id ?? ""} envVar="MULTICA_WORKSPACE_ID" />
            {workspace?.name ? (
              <CopyableField label="Workspace name" value={workspace.name} />
            ) : null}
          </CardContent>
        </Card>
      </section>

      {/* ─── Section 3: MCP server setup ───────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Sparkles className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Model Context Protocol server</h2>
        </div>
        <Card>
          <CardContent className="space-y-4 text-sm">
            <p className="text-xs text-muted-foreground">
              The <code className="rounded bg-muted px-1.5 py-0.5 text-[11px]">@multica/mcp</code>{" "}
              server lets any{" "}
              <a
                href="https://modelcontextprotocol.io"
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-0.5 underline underline-offset-2"
              >
                MCP-aware <ExternalLink className="h-3 w-3" />
              </a>{" "}
              AI assistant — Claude Code, Claude Desktop, Cursor, Windsurf
              — orchestrate this workspace from chat. It exposes ~32 tools
              spanning issues, agents, channels, projects, labels, and
              autopilots, and maps directly onto the Multica REST API.
            </p>

            <Step
              n={1}
              title="Create a token"
              body="Use the section above. Name it something memorable like 'MCP server' so you can revoke it later if needed."
            />

            <Step
              n={2}
              title="Download the MCP server"
              body={
                <div className="space-y-2">
                  <p className="text-xs text-muted-foreground">
                    The server is bundled with this Multica install. One
                    file, no build step. The version always matches the
                    server it&apos;s talking to so there&apos;s no skew between
                    the API and what the MCP exposes.
                  </p>
                  <CodeBlock text={buildDownloadCommand()} />
                </div>
              }
            />

            <Step
              n={3}
              title="Register with your AI assistant"
              body={
                <div className="space-y-3">
                  <ClientHeading>Claude Code</ClientHeading>
                  <CodeBlock
                    text={buildClaudeCodeCommand({
                      apiBaseUrl,
                      workspaceId: workspace?.id ?? "<workspace-id>",
                    })}
                  />
                  <ClientHeading>Claude Desktop</ClientHeading>
                  <p className="text-xs text-muted-foreground">
                    Add to{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-[11px]">
                      ~/Library/Application Support/Claude/claude_desktop_config.json
                    </code>{" "}
                    (macOS) and restart the app. Claude Desktop doesn&apos;t
                    expand <code className="rounded bg-muted px-1 py-0.5 text-[11px]">~</code>{" "}
                    in JSON paths — replace it with your absolute home
                    path (e.g.{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-[11px]">
                      /Users/you/.local/bin/multica-mcp
                    </code>
                    ).
                  </p>
                  <CodeBlock
                    text={buildClaudeDesktopConfig({
                      apiBaseUrl,
                      workspaceId: workspace?.id ?? "<workspace-id>",
                    })}
                  />
                  <ClientHeading>Cursor / Windsurf / other MCP clients</ClientHeading>
                  <p className="text-xs text-muted-foreground">
                    Any client that speaks the standard MCP stdio transport
                    works. The command is{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-[11px]">
                      ~/.local/bin/multica-mcp
                    </code>{" "}
                    plus the three env vars from the snippet above.
                  </p>
                </div>
              }
            />

            <Step
              n={4}
              title="Try it"
              body={
                <p className="text-xs text-muted-foreground">
                  Start a new session in your AI client and prompt with
                  something like &ldquo;<span className="italic">List my open
                  Multica issues</span>&rdquo; — the assistant should call{" "}
                  <code className="rounded bg-muted px-1 py-0.5 text-[11px]">multica_issue_list</code>{" "}
                  and return your issue list. From there it can create new
                  issues, assign agents, post in channels, trigger
                  autopilots — anything the tool list covers.
                </p>
              }
            />

            <p className="rounded-md border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
              <strong className="text-foreground">Security:</strong> the
              MCP server runs on your local machine and acts on behalf of
              whoever owns the token. Treat the token like a password and
              revoke it if it leaks. Mutating tools (issue create/update,
              channel post, autopilot trigger) all run with your account&apos;s
              permissions — there is no separate MCP-only scope.
            </p>
          </CardContent>
        </Card>
      </section>

      {/* ─── Section 4: REST API reference ─────────────────────────── */}
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Code2 className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">REST API</h2>
        </div>
        <Card>
          <CardContent className="space-y-5 text-sm">
            <p className="text-xs text-muted-foreground">
              Everything the web app, CLI, and MCP server do is available
              over plain HTTPS. Endpoints follow REST conventions: list /
              get / create / update / delete on resource paths, JSON in
              and out, bearer-token auth.
            </p>

            <div className="space-y-2">
              <h3 className="text-xs font-medium">Authentication</h3>
              <p className="text-xs text-muted-foreground">
                Send your personal access token as a{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">Bearer</code>{" "}
                header. Workspace-scoped endpoints also need an{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">X-Workspace-ID</code>{" "}
                header — without it the API can&apos;t resolve which
                workspace the call should target. Write requests use{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">application/json</code>.
              </p>
              <CodeBlock
                text={buildCurlSample({
                  apiBaseUrl,
                  workspaceId: workspace?.id ?? "<workspace-id>",
                })}
              />
            </div>

            <div className="space-y-2">
              <h3 className="text-xs font-medium">Endpoints</h3>
              <p className="text-xs text-muted-foreground">
                Paths are relative to the API base URL above. Path
                parameters are{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">
                  {"{like-this}"}
                </code>
                ; everything else is a literal segment. Where a path has
                two methods listed, both are supported on the same URL.
              </p>

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
                <strong className="text-foreground">Conventions:</strong>{" "}
                list endpoints support{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">limit</code>{" "}
                and{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">offset</code>{" "}
                query params (default 50, max 200). Channel and chat
                timelines paginate by{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">before</code>{" "}
                (RFC3339 timestamp, exclusive) instead of offset because
                rapid inserts at the head would otherwise cause skips.
                Errors return{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">{"{ error: \"…\" }"}</code>{" "}
                with the appropriate 4xx / 5xx status. Empty responses
                use 204 (no body).
              </p>
              <p>
                <strong className="text-foreground">No OpenAPI spec yet:</strong>{" "}
                the canonical reference is the route table at{" "}
                <code className="rounded bg-muted px-1 py-0.5 text-[11px]">
                  server/cmd/server/router.go
                </code>{" "}
                in the source tree. The list above covers what the web
                app, CLI, and MCP server use day-to-day.
              </p>
            </div>
          </CardContent>
        </Card>
      </section>

      <AlertDialog open={!!revokeConfirmId} onOpenChange={(v) => { if (!v) setRevokeConfirmId(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Revoke token</AlertDialogTitle>
            <AlertDialogDescription>
              This token will be permanently revoked and can no longer be used. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={async () => {
                if (revokeConfirmId) await handleRevokeToken(revokeConfirmId);
                setRevokeConfirmId(null);
              }}
            >
              Revoke
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={!!newToken} onOpenChange={(v) => { if (!v) { setNewToken(null); setTokenCopied(false); } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Token created</DialogTitle>
            <DialogDescription>
              Copy your personal access token now. You won&apos;t be able to see it again.
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
              <TooltipContent>Copy token</TooltipContent>
            </Tooltip>
          </div>
          <DialogFooter>
            <Button onClick={() => { setNewToken(null); setTokenCopied(false); }}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────────────────
// Sub-components — kept colocated since they're trivial and only used here.
// ─────────────────────────────────────────────────────────────────────────

function CopyableField({ label, value, envVar }: { label: string; value: string; envVar?: string }) {
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
          {value || <span className="text-muted-foreground">unavailable</span>}
        </code>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="outline"
                size="icon-sm"
                onClick={copy}
                disabled={!value}
                aria-label={`Copy ${label}`}
              >
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              </Button>
            }
          />
          <TooltipContent>Copy</TooltipContent>
        </Tooltip>
      </div>
    </div>
  );
}

function CodeBlock({ text }: { text: string }) {
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
              className="absolute right-1.5 top-1.5"
              onClick={copy}
              aria-label="Copy snippet"
            >
              {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
            </Button>
          }
        />
        <TooltipContent>Copy</TooltipContent>
      </Tooltip>
    </div>
  );
}

function Step({ n, title, body }: { n: number; title: string; body: React.ReactNode }) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/15 text-[11px] font-semibold text-primary">
          {n}
        </span>
        <span className="text-sm font-medium">{title}</span>
      </div>
      <div className="ml-7 space-y-2">
        {typeof body === "string" ? (
          <p className="text-xs text-muted-foreground">{body}</p>
        ) : (
          body
        )}
      </div>
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

// REST API endpoint reference helpers. Kept as small presentational
// components colocated with the tab — they're not used elsewhere.

function EndpointGroup({ title, children }: { title: string; children: React.ReactNode }) {
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

// `method` accepts a slash-joined string like "GET / POST" so a single
// row can describe all the verbs supported on the same path. Each verb
// gets its own coloured badge for scannability.
function EndpointRow({ method, path, desc }: { method: string; path: string; desc: string }) {
  const methods = method.split("/").map((m) => m.trim()).filter(Boolean);
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] items-baseline gap-x-3 gap-y-1 px-3 py-2 sm:grid-cols-[auto_minmax(0,2fr)_minmax(0,3fr)]">
      <div className="flex flex-wrap items-center gap-1">
        {methods.map((m) => (
          <MethodBadge key={m} method={m} />
        ))}
      </div>
      <code className="col-span-1 text-[11px] sm:text-xs break-all font-mono">{path}</code>
      <p className="col-span-2 text-[11px] text-muted-foreground sm:col-span-1">{desc}</p>
    </div>
  );
}

function MethodBadge({ method }: { method: string }) {
  const m = method.toUpperCase();
  // Coloring follows common API-doc conventions (Stripe, GitHub):
  // GET=blue, POST=green, PUT=amber, PATCH=violet, DELETE=red. Using
  // utility classes directly keeps the badge lightweight without adding
  // a new shadcn variant.
  const color =
    m === "GET" ? "bg-blue-500/15 text-blue-700 dark:text-blue-300" :
    m === "POST" ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300" :
    m === "PUT" ? "bg-amber-500/15 text-amber-700 dark:text-amber-300" :
    m === "PATCH" ? "bg-violet-500/15 text-violet-700 dark:text-violet-300" :
    m === "DELETE" ? "bg-red-500/15 text-red-700 dark:text-red-300" :
    "bg-muted text-foreground";
  return (
    <span className={`inline-flex min-w-[44px] items-center justify-center rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide ${color}`}>
      {m}
    </span>
  );
}

// Snippet builders — kept in the component file so the wording stays
// next to the surrounding copy. If MCP setup ever surfaces in two places
// (e.g. an empty-state in the channels page) we can lift these into a
// shared module.

// Where the downloaded MCP server lands on the user's machine. Picked
// `~/.local/bin` because it's on PATH for most Linux/macOS shell setups
// and doesn't require sudo.
const MCP_INSTALL_PATH = "~/.local/bin/multica-mcp";

// Frontend serves the bundled MCP server as a static asset under
// /multica-mcp.js. Using `window.location.origin` means the download
// always points at the same Multica install the user is currently
// browsing — no version skew between the running API and the MCP
// server's tool definitions.
function buildDownloadCommand(): string {
  const origin =
    typeof window !== "undefined" && window.location?.origin
      ? window.location.origin
      : "<frontend-url>";
  return [
    "mkdir -p ~/.local/bin",
    `curl -fsSL ${origin}/multica-mcp.js -o ${MCP_INSTALL_PATH}`,
    `chmod +x ${MCP_INSTALL_PATH}`,
  ].join(" && \\\n");
}

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
    `  -e MULTICA_API_URL=${url} \\`,
    "  -e MULTICA_TOKEN=mul_… \\",
    `  -e MULTICA_WORKSPACE_ID=${workspaceId} \\`,
    `  -- ${MCP_INSTALL_PATH}`,
  ].join("\n");
}

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
          command: MCP_INSTALL_PATH,
          env: {
            MULTICA_API_URL: url,
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
