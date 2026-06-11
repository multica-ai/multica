"use client";

import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2, MessageSquare, ChevronRight } from "lucide-react";
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
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { cn } from "@multica/ui/lib/utils";
import { memberListOptions } from "@multica/core/workspace/queries";
import { octoInstallationsOptions, octoKeys } from "@multica/core/octo";
import { api, ApiError } from "@multica/core/api";
import type { OctoInstallation } from "@multica/core/types";
import { useT } from "../../i18n";

// OctoTab is the workspace settings panel for Octo IM bot installations.
// Listing is member-visible; configure / disconnect are admin-only (the backend
// enforces it via RequireWorkspaceRole; the UI hides the actions to match).
//
// Unlike Lark (device-flow QR scan), Octo configures with a single bf_* bot
// token, so the "Configure a bot" dialog is just an agent id + token form.
export function OctoTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: listing, isLoading } = useQuery({
    ...octoInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });

  const isAdmin = members.some(
    (m) => m.user_id === user?.id && (m.role === "owner" || m.role === "admin"),
  );

  const installations = listing?.installations ?? [];
  const configured = listing?.configured === true;

  const [configureOpen, setConfigureOpen] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<OctoInstallation | null>(null);

  const refresh = () => {
    if (wsId) qc.invalidateQueries({ queryKey: octoKeys.installations(wsId) });
  };

  // Localize the known installation statuses; an unknown value (a status the
  // backend adds later) downgrades to its raw string rather than crashing —
  // enum drift downgrades, never throws (CLAUDE.md → API Response Compatibility).
  const statusText = (status: string) => {
    switch (status) {
      case "active":
        return t(($) => $.octo.status_active);
      case "revoked":
        return t(($) => $.octo.status_revoked);
      default:
        return status;
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">{t(($) => $.octo.section_title)}</h3>
          <p className="text-sm text-muted-foreground">{t(($) => $.octo.description)}</p>
        </div>
        {isAdmin && configured && (
          <Button size="sm" onClick={() => setConfigureOpen(true)}>
            {t(($) => $.octo.configure)}
          </Button>
        )}
      </div>

      {!configured && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">
            {t(($) => $.octo.not_configured)}
          </CardContent>
        </Card>
      )}

      {configured && isLoading && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">{t(($) => $.octo.loading)}</CardContent>
        </Card>
      )}

      {configured && !isLoading && installations.length === 0 && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">{t(($) => $.octo.empty)}</CardContent>
        </Card>
      )}

      {installations.map((inst) => (
        <Card key={inst.id}>
          <CardContent className="flex items-center justify-between py-4">
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{inst.bot_name || inst.robot_id}</div>
              <div className="truncate text-xs text-muted-foreground">
                {t(($) => $.octo.status_label)}: {statusText(inst.status)}
              </div>
            </div>
            {isAdmin && (
              <Button
                size="icon"
                variant="ghost"
                aria-label={t(($) => $.octo.disconnect)}
                onClick={() => setRevokeTarget(inst)}
              >
                <Trash2 className="size-4" />
              </Button>
            )}
          </CardContent>
        </Card>
      ))}

      <ConfigureDialog
        open={configureOpen}
        onOpenChange={setConfigureOpen}
        wsId={wsId}
        onConfigured={() => {
          setConfigureOpen(false);
          refresh();
        }}
      />

      <AlertDialog open={!!revokeTarget} onOpenChange={(o) => !o && setRevokeTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.octo.disconnect_title)}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.octo.disconnect_body)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.octo.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                const target = revokeTarget;
                setRevokeTarget(null);
                if (!target || !wsId) return;
                try {
                  await api.deleteOctoInstallation(wsId, target.id);
                  toast.success(t(($) => $.octo.disconnected));
                  refresh();
                } catch (err) {
                  toast.error(err instanceof ApiError ? err.message : t(($) => $.octo.disconnect_failed));
                }
              }}
            >
              {t(($) => $.octo.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// OctoAgentBindButton is the per-agent entry point we surface from the agent
// detail page (inspector + Integrations tab). The Settings OctoTab is the
// management view; this button is the shortcut for "connect a bot to THIS
// agent". Visibility mirrors OctoTab's gates:
//   1. Only workspace owners/admins (the backend gates create/delete on role).
//   2. Only when the deployment has Octo enabled (configured).
//   3. If this agent already has an active installation, show a connected
//      status row; otherwise show the Connect CTA, which opens the shared
//      ConfigureDialog pre-filled and locked to this agent id.
export function OctoAgentBindButton({
  agentId,
  agentName,
  className,
  onShowConnectedDetails,
}: {
  agentId: string;
  agentName?: string;
  className?: string;
  /**
   * When set, the connected state renders as a compact status row that invokes
   * this callback (the inspector passes a "jump to the Integrations tab"
   * handler) instead of a plain label. Mirrors LarkAgentBindButton.
   */
  onShowConnectedDetails?: () => void;
}) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  const [configureOpen, setConfigureOpen] = useState(false);

  const { data: listing } = useQuery({
    ...octoInstallationsOptions(wsId),
    enabled: !!wsId,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: !!wsId,
  });

  const canManage = members.some(
    (m) => m.user_id === user?.id && (m.role === "owner" || m.role === "admin"),
  );
  if (!canManage) return null;
  if (listing?.configured !== true) return null;

  const existing = listing.installations.find(
    (inst) => inst.agent_id === agentId && inst.status === "active",
  );
  if (existing) {
    return (
      <OctoAgentConnectedRow
        installation={existing}
        onClick={onShowConnectedDetails}
        className={className}
      />
    );
  }

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className={className}
        onClick={() => setConfigureOpen(true)}
        disabled={!agentId}
        title={
          agentName ? t(($) => $.octo.bind_button_title, { agent: agentName }) : undefined
        }
        data-testid="octo-agent-bind"
      >
        <MessageSquare className="h-3 w-3" />
        {t(($) => $.octo.bind_button)}
      </Button>
      <ConfigureDialog
        open={configureOpen}
        onOpenChange={setConfigureOpen}
        wsId={wsId}
        defaultAgentId={agentId}
        onConfigured={() => {
          setConfigureOpen(false);
          if (wsId) qc.invalidateQueries({ queryKey: octoKeys.installations(wsId) });
        }}
      />
    </>
  );
}

// OctoAgentConnectedRow is the compact "already connected" affordance shown in
// place of the Connect button when this agent has an active Octo installation.
// When onClick is provided (the inspector) it's a button that deep-links into
// the Integrations tab; otherwise it's a static status label.
function OctoAgentConnectedRow({
  installation,
  onClick,
  className,
}: {
  installation: OctoInstallation;
  onClick?: () => void;
  className?: string;
}) {
  const { t } = useT("settings");
  const label = installation.bot_name || installation.robot_id;
  const content = (
    <>
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />
      <span className="truncate">
        {t(($) => $.octo.agent_connected_label)}
        {label ? ` · ${label}` : ""}
      </span>
      {onClick && <ChevronRight className="ml-auto h-3.5 w-3.5 shrink-0" />}
    </>
  );
  if (!onClick) {
    return (
      <div
        className={cn(
          "flex items-center gap-2 text-xs text-muted-foreground",
          className,
        )}
        data-testid="octo-agent-connected"
      >
        {content}
      </div>
    );
  }
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs text-muted-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        className,
      )}
      data-testid="octo-agent-connected"
    >
      {content}
    </button>
  );
}

function ConfigureDialog({
  open,
  onOpenChange,
  wsId,
  onConfigured,
  defaultAgentId,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  wsId: string;
  onConfigured: () => void;
  /**
   * When set, the agent-id field is pre-filled and locked — used by the
   * per-agent bind entry point so an admin can't accidentally bind the bot to
   * a different agent than the one they opened the dialog from. Omitted by the
   * Settings panel, which lets the admin type any agent id.
   */
  defaultAgentId?: string;
}) {
  const { t } = useT("settings");
  const [agentId, setAgentId] = useState(defaultAgentId ?? "");
  const [botToken, setBotToken] = useState("");
  const [apiUrl, setApiUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);

  // Keep the field in sync when the dialog is reopened for a different agent
  // (the component stays mounted across opens in the per-agent button).
  useEffect(() => {
    if (open) setAgentId(defaultAgentId ?? "");
  }, [open, defaultAgentId]);

  const submit = async () => {
    if (!agentId.trim() || !botToken.trim()) return;
    setSubmitting(true);
    try {
      await api.createOctoInstallation(wsId, {
        agent_id: agentId.trim(),
        bot_token: botToken.trim(),
        api_url: apiUrl.trim() || undefined,
      });
      toast.success(t(($) => $.octo.configured));
      setAgentId(defaultAgentId ?? "");
      setBotToken("");
      setApiUrl("");
      onConfigured();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : t(($) => $.octo.configure_failed));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.octo.configure)}</DialogTitle>
          <DialogDescription>{t(($) => $.octo.configure_desc)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <label className="text-xs font-medium">{t(($) => $.octo.agent_id)}</label>
            <Input
              value={agentId}
              onChange={(e) => setAgentId(e.target.value)}
              placeholder={t(($) => $.octo.agent_id_placeholder)}
              disabled={!!defaultAgentId}
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium">{t(($) => $.octo.bot_token)}</label>
            <Input value={botToken} onChange={(e) => setBotToken(e.target.value)} placeholder={t(($) => $.octo.bot_token_placeholder)} />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium">{t(($) => $.octo.api_url)}</label>
            <Input value={apiUrl} onChange={(e) => setApiUrl(e.target.value)} placeholder={t(($) => $.octo.api_url_placeholder)} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.octo.cancel)}
          </Button>
          <Button disabled={submitting || !agentId.trim() || !botToken.trim()} onClick={submit}>
            {submitting ? t(($) => $.octo.saving) : t(($) => $.octo.configure)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
