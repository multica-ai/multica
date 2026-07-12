"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, Check, Loader2, Plug, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
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
import { api } from "@multica/core/api";
import {
  composioConnectionsOptions,
  composioKeys,
  composioToolkitsOptions,
} from "@multica/core/composio";
import type { ComposioToolkit } from "@multica/core/types";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { useT, useTimeAgo } from "../../i18n";
import { useNavigation } from "../../navigation";

// ComposioTab renders the connectable Composio toolkit catalog and lets the
// user connect / disconnect the apps their agents can act on.
//
// Key UX rule (MUL-4009): the backend only returns toolkits with an enabled
// auth config in the Composio project, so every card here is connectable —
// toolkits with no auth config are filtered out server-side rather than shown
// with a dead "not configured" hint. The `toolkit.connectable` guard on the
// Connect button is kept as a client-side backstop (older/misbehaving servers
// could still send a non-connectable entry); such an entry simply renders no
// action affordance rather than a broken Connect button that would 400.
export function ComposioTab() {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const navigation = useNavigation();

  const toolkitsQuery = useQuery(composioToolkitsOptions());
  const connectionsQuery = useQuery(composioConnectionsOptions());

  const [query, setQuery] = useState("");
  const [connectingSlug, setConnectingSlug] = useState<string | null>(null);
  const [disconnectTarget, setDisconnectTarget] = useState<{
    connectionId: string;
    name: string;
  } | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  // The hosted Composio consent flow is a full-page redirect that lands back
  // on the settings page carrying either `?connected=<slug>` (success) or
  // `?error=composio_connect_failed` (any backend-side failure — see
  // Service.CallbackRedirect, MUL-3720). Consume it exactly once: fire a toast,
  // refresh the connections list so the freshly-linked card flips to Connected
  // without a manual reload, then strip the one-shot params via `replace` so a
  // browser refresh doesn't re-toast.
  const connectedParam = navigation.searchParams.get("connected");
  const errorParam = navigation.searchParams.get("error");
  // React Strict Mode (dev / Next) double-invokes mount effects as
  // mount → cleanup → mount. On the second invoke the `replace` from the first
  // hasn't committed yet, so the closure still sees the same params and would
  // toast + invalidate twice. Guard with a ref keyed on the callback we already
  // consumed; a genuinely new callback (different slug, or the redirect being a
  // full page load that resets this ref) still fires.
  const consumedCallbackKey = useRef<string | null>(null);
  useEffect(() => {
    const callbackKey = connectedParam
      ? `connected:${connectedParam}`
      : errorParam === "composio_connect_failed"
        ? "error:composio_connect_failed"
        : null;
    if (!callbackKey) return;
    if (consumedCallbackKey.current === callbackKey) return;
    consumedCallbackKey.current = callbackKey;
    if (connectedParam) {
      toast.success(t(($) => $.composio.toast_connected));
      void qc.invalidateQueries({ queryKey: composioKeys.connections() });
    } else {
      toast.error(t(($) => $.composio.toast_connect_failed));
    }
    // Drop only the Composio one-shot params; keep everything else (notably
    // ?tab=integrations) so the user stays on this tab.
    const params = new URLSearchParams(navigation.searchParams);
    params.delete("connected");
    params.delete("error");
    const qs = params.toString();
    navigation.replace(qs ? `${navigation.pathname}?${qs}` : navigation.pathname);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connectedParam, errorParam]);

  // Map active connections by toolkit slug so each card knows whether it is
  // already connected (and which connection id to disconnect).
  const connectionBySlug = useMemo(() => {
    const m = new Map<string, string>();
    for (const c of connectionsQuery.data ?? []) {
      if (c.status === "active") m.set(c.toolkit_slug, c.id);
    }
    return m;
  }, [connectionsQuery.data]);

  // Toolkits whose latest connection is expired render a Reconnect affordance
  // instead of Connected/Connect. Backend only emits `expired` once Stage 4
  // (MUL-3719) lands, but the branch is wired up now so it lights up for free.
  const expiredBySlug = useMemo(() => {
    const m = new Set<string>();
    for (const c of connectionsQuery.data ?? []) {
      if (c.status === "expired") m.add(c.toolkit_slug);
    }
    return m;
  }, [connectionsQuery.data]);

  // Last-used timestamp per active connection, for the "Last used …" line on a
  // connected card. Backend leaves this null until tool-call dispatch starts
  // stamping it (Stage 3, MUL-3721); the card shows a "never used" placeholder
  // until then.
  const lastUsedBySlug = useMemo(() => {
    const m = new Map<string, string | null>();
    for (const c of connectionsQuery.data ?? []) {
      if (c.status === "active") m.set(c.toolkit_slug, c.last_used_at ?? null);
    }
    return m;
  }, [connectionsQuery.data]);

  const toolkits = useMemo(() => toolkitsQuery.data ?? [], [toolkitsQuery.data]);
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return toolkits;
    return toolkits.filter(
      (tk) =>
        tk.name.toLowerCase().includes(q) ||
        tk.slug.toLowerCase().includes(q) ||
        (tk.category ?? "").toLowerCase().includes(q),
    );
  }, [toolkits, query]);

  // 503 handling lives in the parent IntegrationsTab, which hides the whole
  // Composio section when COMPOSIO_API_KEY is unset — this component only
  // mounts when the integration is configured, so it deals with the loaded /
  // error / empty / list states below.

  async function handleConnect(tk: ComposioToolkit) {
    if (connectingSlug) return;
    setConnectingSlug(tk.slug);
    try {
      const { redirect_url } = await api.beginComposioConnect(tk.slug);
      // Hand the browser to Composio's hosted consent flow; it redirects back
      // to /api/integrations/composio/callback when done.
      window.location.href = redirect_url;
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.composio.connect_failed));
      setConnectingSlug(null);
    }
  }

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteComposioConnection(disconnectTarget.connectionId);
      await qc.invalidateQueries({ queryKey: composioKeys.connections() });
      toast.success(t(($) => $.composio.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.composio.disconnect_failed));
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-6">
      <section className="space-y-1">
        <p className="text-sm text-muted-foreground">{t(($) => $.composio.page_description)}</p>
      </section>

      {toolkitsQuery.isLoading ? (
        <Card>
          <CardContent>
            <p className="text-sm text-muted-foreground">{t(($) => $.composio.loading)}</p>
          </CardContent>
        </Card>
      ) : toolkitsQuery.isError ? (
        <Card>
          <CardContent>
            <p className="text-sm text-destructive">{t(($) => $.composio.load_failed)}</p>
          </CardContent>
        </Card>
      ) : toolkits.length === 0 ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-sm font-medium">{t(($) => $.composio.empty_title)}</p>
            <p className="text-xs text-muted-foreground">{t(($) => $.composio.empty_description)}</p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t(($) => $.composio.search_placeholder)}
            className="max-w-xs"
          />
          {connectionsQuery.isError && (
            // Don't silently treat a failed connections fetch as "nothing
            // connected" — that would hide real connections and offer Connect
            // on something already linked. Surface it so the user knows the
            // connected state may be incomplete; the catalog still renders.
            <p className="text-xs text-destructive">
              {t(($) => $.composio.connections_load_failed)}
            </p>
          )}
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {filtered.map((tk) => (
              <ToolkitCard
                key={tk.slug}
                toolkit={tk}
                connectionId={connectionBySlug.get(tk.slug)}
                expired={expiredBySlug.has(tk.slug)}
                lastUsedAt={lastUsedBySlug.get(tk.slug) ?? null}
                connecting={connectingSlug === tk.slug}
                anyConnecting={connectingSlug !== null}
                onConnect={() => handleConnect(tk)}
                onDisconnect={(connectionId, name) =>
                  setDisconnectTarget({ connectionId, name })
                }
              />
            ))}
          </div>
        </section>
      )}

      <AlertDialog
        open={!!disconnectTarget}
        onOpenChange={(v) => {
          if (!v && !disconnecting) setDisconnectTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.composio.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.composio.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.composio.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction onClick={handleDisconnect} disabled={disconnecting}>
              {disconnecting
                ? t(($) => $.composio.disconnecting)
                : t(($) => $.composio.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function ToolkitCard({
  toolkit,
  connectionId,
  expired,
  lastUsedAt,
  connecting,
  anyConnecting,
  onConnect,
  onDisconnect,
}: {
  toolkit: ComposioToolkit;
  connectionId?: string;
  expired: boolean;
  lastUsedAt: string | null;
  connecting: boolean;
  anyConnecting: boolean;
  onConnect: () => void;
  onDisconnect: (connectionId: string, name: string) => void;
}) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const isConnected = !!connectionId;

  return (
    <Card>
      <CardContent className="flex items-center gap-3 p-3">
        <ComposioToolkitLogo
          slug={toolkit.slug}
          name={toolkit.name}
          fallbackLogo={toolkit.logo}
        />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium">{toolkit.name || toolkit.slug}</p>
          {isConnected ? (
            // Last-used line. Backend leaves last_used_at null until Stage 3
            // dispatch stamps it, so show a localized "never used" placeholder
            // rather than hiding the line entirely.
            <p className="truncate text-[10px] text-muted-foreground">
              {lastUsedAt
                ? t(($) => $.composio.last_used, { when: timeAgo(lastUsedAt) })
                : t(($) => $.composio.last_used_never)}
            </p>
          ) : toolkit.category ? (
            <p className="truncate text-[10px] uppercase tracking-wide text-muted-foreground">
              {toolkit.category}
            </p>
          ) : null}
        </div>

        {isConnected ? (
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center gap-1 text-xs text-emerald-600">
              <Check className="h-3 w-3" />
              {t(($) => $.composio.connected)}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => onDisconnect(connectionId!, toolkit.name || toolkit.slug)}
              aria-label={t(($) => $.composio.disconnect)}
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          </div>
        ) : expired ? (
          // Token-expired connection: surface the failure and let the user
          // re-run the same connect flow in one click (no disconnect step).
          <div className="flex items-center gap-2">
            <span className="inline-flex items-center gap-1 text-xs text-amber-600">
              <AlertTriangle className="h-3 w-3" />
              {t(($) => $.composio.expired)}
            </span>
            <Button size="sm" variant="outline" onClick={onConnect} disabled={anyConnecting}>
              {connecting ? (
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <RefreshCw className="h-3 w-3" />
              )}
              {connecting ? t(($) => $.composio.connecting) : t(($) => $.composio.reconnect)}
            </Button>
          </div>
        ) : toolkit.connectable ? (
          <Button size="sm" onClick={onConnect} disabled={anyConnecting}>
            {connecting ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Plug className="h-3 w-3" />
            )}
            {connecting ? t(($) => $.composio.connecting) : t(($) => $.composio.connect)}
          </Button>
        ) : null}
      </CardContent>
    </Card>
  );
}
