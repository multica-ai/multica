"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Server, Trash2, Copy, Check } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
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
import { copyText } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  SERVICE_TOKEN_SCOPES,
  serviceTokenListSchema,
  createServiceTokenSchema,
  type ServiceToken,
  type CreateServiceTokenResponse,
} from "../types";

const EXPIRY_OPTIONS: { value: string; label: string }[] = [
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "365", label: "1 year" },
];

/**
 * ServiceTokensSection renders the FIR-3608 service-token management surface
 * inside the existing Settings → Tokens tab: create, list, and revoke
 * workspace-bound, scoped `msv_` API keys. Management is owner/admin only
 * (enforced server-side); non-managers see a read-only explanation.
 */
export function ServiceTokensSection() {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const canManage = useMemo(() => {
    const role = members.find((m) => m.user_id === user?.id)?.role;
    return role === "owner" || role === "admin";
  }, [members, user?.id]);

  const [tokens, setTokens] = useState<ServiceToken[]>([]);
  const [tokensLoading, setTokensLoading] = useState(true);
  const [tokenName, setTokenName] = useState("");
  const [tokenExpiry, setTokenExpiry] = useState("90");
  const [selectedScopes, setSelectedScopes] = useState<string[]>(["skills:read"]);
  const [tokenCreating, setTokenCreating] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);
  const [tokenCopied, setTokenCopied] = useState(false);
  const [tokenRevoking, setTokenRevoking] = useState<string | null>(null);
  const [revokeConfirmId, setRevokeConfirmId] = useState<string | null>(null);

  const loadTokens = useCallback(async () => {
    if (!canManage) {
      setTokensLoading(false);
      return;
    }
    try {
      const raw = await api.listServiceTokens();
      setTokens(
        parseWithFallback<ServiceToken[]>(raw, serviceTokenListSchema, [], {
          endpoint: "GET /api/service-tokens",
        }),
      );
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to load service tokens");
    } finally {
      setTokensLoading(false);
    }
  }, [canManage]);

  useEffect(() => {
    loadTokens();
  }, [loadTokens]);

  const toggleScope = (scope: string) => {
    setSelectedScopes((prev) =>
      prev.includes(scope) ? prev.filter((s) => s !== scope) : [...prev, scope],
    );
  };

  const handleCreateToken = async () => {
    setTokenCreating(true);
    try {
      const raw = await api.createServiceToken({
        name: tokenName.trim(),
        scopes: selectedScopes,
        expires_in_days: Number(tokenExpiry),
      });
      const result = parseWithFallback<CreateServiceTokenResponse | null>(
        raw,
        createServiceTokenSchema,
        null,
        { endpoint: "POST /api/service-tokens" },
      );
      if (result?.token) {
        setNewToken(result.token);
      }
      setTokenName("");
      setTokenExpiry("90");
      setSelectedScopes(["skills:read"]);
      await loadTokens();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create service token");
    } finally {
      setTokenCreating(false);
    }
  };

  const handleRevokeToken = async (id: string) => {
    setTokenRevoking(id);
    try {
      await api.revokeServiceToken(id);
      await loadTokens();
      toast.success("Service token revoked");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to revoke service token");
    } finally {
      setTokenRevoking(null);
    }
  };

  const handleCopyToken = async () => {
    if (!newToken) return;
    if (await copyText(newToken)) {
      setTokenCopied(true);
      setTimeout(() => setTokenCopied(false), 2000);
    }
  };

  const createDisabled = tokenCreating || !tokenName.trim() || selectedScopes.length === 0;

  return (
    <section className="space-y-4">
      <div className="flex items-center gap-2">
        <Server className="h-4 w-4 text-muted-foreground" />
        <h2 className="text-sm font-semibold">Service tokens</h2>
      </div>

      <Card>
        <CardContent className="space-y-3">
          <p className="text-xs text-muted-foreground">
            Workspace-bound API keys for external systems and agents. Each key is scoped to
            least-privilege access, expires, and can be revoked here. The secret is shown once.
          </p>

          {!canManage ? (
            <p className="text-xs text-muted-foreground">
              Only workspace owners and admins can manage service tokens.
            </p>
          ) : (
            <>
              <div className="grid gap-3 sm:grid-cols-[1fr_120px_auto]">
                <Input
                  type="text"
                  value={tokenName}
                  onChange={(e) => setTokenName(e.target.value)}
                  placeholder="Token name (e.g. Atlas read-only)"
                />
                <Select value={tokenExpiry} onValueChange={(v) => { if (v) setTokenExpiry(v); }}>
                  <SelectTrigger size="sm"><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {EXPIRY_OPTIONS.map((opt) => (
                      <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Button onClick={handleCreateToken} disabled={createDisabled}>
                  {tokenCreating ? "Creating…" : "Create"}
                </Button>
              </div>

              <div className="space-y-1.5">
                <div className="text-xs font-medium text-muted-foreground">Scopes</div>
                <div className="flex flex-wrap gap-2">
                  {SERVICE_TOKEN_SCOPES.map((scope) => {
                    const active = selectedScopes.includes(scope);
                    return (
                      <Button
                        key={scope}
                        type="button"
                        size="sm"
                        variant={active ? "default" : "outline"}
                        aria-pressed={active}
                        onClick={() => toggleScope(scope)}
                      >
                        {scope}
                      </Button>
                    );
                  })}
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {canManage && (tokensLoading ? (
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
      ) : tokens.length > 0 ? (
        <div className="space-y-2">
          {tokens.map((token) => (
            <Card key={token.id}>
              <CardContent className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium truncate">{token.name}</span>
                    {token.revoked && (
                      <span className="text-xs text-muted-foreground">(revoked)</span>
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {token.token_prefix} · scopes: {token.scopes.join(", ") || "none"} · created{" "}
                    {new Date(token.created_at).toLocaleDateString()}
                    {token.last_used_at
                      ? ` · last used ${new Date(token.last_used_at).toLocaleDateString()}`
                      : " · never used"}
                    {token.expires_at
                      ? ` · expires ${new Date(token.expires_at).toLocaleDateString()}`
                      : ""}
                  </div>
                </div>
                {!token.revoked && (
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setRevokeConfirmId(token.id)}
                          disabled={tokenRevoking === token.id}
                          aria-label={`Revoke ${token.name}`}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      }
                    />
                    <TooltipContent>Revoke token</TooltipContent>
                  </Tooltip>
                )}
              </CardContent>
            </Card>
          ))}
        </div>
      ) : (
        <Card>
          <CardContent className="text-xs text-muted-foreground">
            No service tokens yet.
          </CardContent>
        </Card>
      ))}

      <AlertDialog open={!!revokeConfirmId} onOpenChange={(v) => { if (!v) setRevokeConfirmId(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Revoke service token?</AlertDialogTitle>
            <AlertDialogDescription>
              Any system using this token will immediately lose access. This cannot be undone.
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
            <DialogTitle>Service token created</DialogTitle>
            <DialogDescription>
              Copy this token now — it will not be shown again.
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
    </section>
  );
}
