"use client";

import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Check, Loader2, Plug, Trash2 } from "lucide-react";
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
import { useT } from "../../i18n";

// ComposioTab renders the full Composio toolkit catalog and lets the user
// connect / disconnect the apps their agents can act on.
//
// Key UX rule (MUL-3720): listing ≠ connectable. Only toolkits with an enabled
// auth config in the Composio project carry `connectable: true`; the rest get a
// muted "not configured" hint instead of a dead Connect button that would 400.
export function ComposioTab() {
  const { t } = useT("settings");
  const qc = useQueryClient();

  const toolkitsQuery = useQuery(composioToolkitsOptions());
  const connectionsQuery = useQuery(composioConnectionsOptions());

  const [query, setQuery] = useState("");
  const [connectingSlug, setConnectingSlug] = useState<string | null>(null);
  const [disconnectTarget, setDisconnectTarget] = useState<{
    connectionId: string;
    name: string;
  } | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  // Map active connections by toolkit slug so each card knows whether it is
  // already connected (and which connection id to disconnect).
  const connectionBySlug = useMemo(() => {
    const m = new Map<string, string>();
    for (const c of connectionsQuery.data ?? []) {
      if (c.status === "active") m.set(c.toolkit_slug, c.id);
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
  connecting,
  anyConnecting,
  onConnect,
  onDisconnect,
}: {
  toolkit: ComposioToolkit;
  connectionId?: string;
  connecting: boolean;
  anyConnecting: boolean;
  onConnect: () => void;
  onDisconnect: (connectionId: string, name: string) => void;
}) {
  const { t } = useT("settings");
  const isConnected = !!connectionId;

  return (
    <Card>
      <CardContent className="flex items-center gap-3 p-3">
        <ToolkitLogo toolkit={toolkit} />
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium">{toolkit.name || toolkit.slug}</p>
          {toolkit.category ? (
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
        ) : toolkit.connectable ? (
          <Button size="sm" onClick={onConnect} disabled={anyConnecting}>
            {connecting ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Plug className="h-3 w-3" />
            )}
            {connecting ? t(($) => $.composio.connecting) : t(($) => $.composio.connect)}
          </Button>
        ) : (
          <span
            className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground"
            title={t(($) => $.composio.not_connectable_hint)}
          >
            {t(($) => $.composio.not_connectable)}
          </span>
        )}
      </CardContent>
    </Card>
  );
}

function ToolkitLogo({ toolkit }: { toolkit: ComposioToolkit }) {
  const initial = (toolkit.name || toolkit.slug).charAt(0).toUpperCase();
  if (toolkit.logo) {
    return (
      <img
        src={toolkit.logo}
        alt=""
        className="h-8 w-8 shrink-0 rounded bg-muted object-contain"
      />
    );
  }
  return (
    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded bg-muted text-xs font-semibold text-muted-foreground">
      {initial}
    </div>
  );
}
