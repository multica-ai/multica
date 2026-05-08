"use client";

import { useEffect, useMemo, useState, useCallback } from "react";
import { Key, Trash2, Copy, Check, Plug, Sparkles } from "lucide-react";
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
          <CardContent className="space-y-4">
            <p className="text-xs text-muted-foreground">
              {t(($) => $.mcp.description)}
            </p>
            {/* Minimal markup intentional — the previous version had a
                multi-step download wizard (separate Node bundle) that
                was the source of a render crash. The Go in-binary MCP
                makes the setup one config snippet. */}
            <div className="space-y-2">
              <div className="text-xs font-medium">
                {t(($) => $.mcp.claude_code_heading)}
              </div>
              <CodeBlock
                text={buildClaudeCodeCommand({
                  apiBaseUrl,
                  workspaceId: workspace?.id ?? "<workspace-id>",
                })}
                copyTooltip={t(($) => $.connection.copy_tooltip)}
              />
            </div>
            <div className="space-y-2">
              <div className="text-xs font-medium">
                {t(($) => $.mcp.claude_desktop_heading)}
              </div>
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
            </div>
            <p className="rounded-md border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
              {t(($) => $.mcp.security_note)}
            </p>
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
