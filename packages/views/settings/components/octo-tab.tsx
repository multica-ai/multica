"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
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
import { memberListOptions } from "@multica/core/workspace/queries";
import { octoInstallationsOptions, octoKeys } from "@multica/core/octo";
import { api, ApiError } from "@multica/core/api";
import type { OctoInstallation } from "@multica/core/types";

// OctoTab is the workspace settings panel for Octo IM bot installations.
// Listing is member-visible; configure / disconnect are admin-only (the backend
// enforces it via RequireWorkspaceRole; the UI hides the actions to match).
//
// Unlike Lark (device-flow QR scan), Octo configures with a single bf_* bot
// token, so the "Configure a bot" dialog is just an agent id + token form.
//
// Copy is intentionally inline English rather than i18n-keyed: this is a new
// integration surface and the locale glossary entries can follow once the copy
// settles. (Keeping it out of the typed `t()` tree avoids shipping half a
// translation set.)
export function OctoTab() {
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

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">Octo IM</h3>
          <p className="text-sm text-muted-foreground">
            Connect Octo bots so agents can chat and create issues from Octo.
          </p>
        </div>
        {isAdmin && configured && (
          <Button size="sm" onClick={() => setConfigureOpen(true)}>
            Configure a bot
          </Button>
        )}
      </div>

      {!configured && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">
            Octo integration is not enabled on this deployment. Ask an operator to set
            MULTICA_OCTO_SECRET_KEY.
          </CardContent>
        </Card>
      )}

      {configured && isLoading && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">Loading…</CardContent>
        </Card>
      )}

      {configured && !isLoading && installations.length === 0 && (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">
            No Octo bots configured yet.
          </CardContent>
        </Card>
      )}

      {installations.map((inst) => (
        <Card key={inst.id}>
          <CardContent className="flex items-center justify-between py-4">
            <div className="min-w-0">
              <div className="truncate text-sm font-medium">{inst.bot_name || inst.robot_id}</div>
              <div className="truncate text-xs text-muted-foreground">Status: {inst.status}</div>
            </div>
            {isAdmin && (
              <Button
                size="icon"
                variant="ghost"
                aria-label="Disconnect"
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
            <AlertDialogTitle>Disconnect this bot?</AlertDialogTitle>
            <AlertDialogDescription>
              The bot stops receiving and sending messages. You can reconfigure it later with the
              same token.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={async () => {
                const target = revokeTarget;
                setRevokeTarget(null);
                if (!target || !wsId) return;
                try {
                  await api.deleteOctoInstallation(wsId, target.id);
                  toast.success("Bot disconnected");
                  refresh();
                } catch (err) {
                  toast.error(err instanceof ApiError ? err.message : "Failed to disconnect");
                }
              }}
            >
              Disconnect
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function ConfigureDialog({
  open,
  onOpenChange,
  wsId,
  onConfigured,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  wsId: string;
  onConfigured: () => void;
}) {
  const [agentId, setAgentId] = useState("");
  const [botToken, setBotToken] = useState("");
  const [apiUrl, setApiUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const submit = async () => {
    if (!agentId.trim() || !botToken.trim()) return;
    setSubmitting(true);
    try {
      await api.createOctoInstallation(wsId, {
        agent_id: agentId.trim(),
        bot_token: botToken.trim(),
        api_url: apiUrl.trim() || undefined,
      });
      toast.success("Bot configured");
      setAgentId("");
      setBotToken("");
      setApiUrl("");
      onConfigured();
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Failed to configure bot");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Configure a bot</DialogTitle>
          <DialogDescription>
            Bind an Octo bot (bf_ token) to an agent. The bot&apos;s identity is fetched from Octo.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="space-y-1">
            <label className="text-xs font-medium">Agent ID</label>
            <Input value={agentId} onChange={(e) => setAgentId(e.target.value)} placeholder="agent uuid" />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium">Bot token</label>
            <Input value={botToken} onChange={(e) => setBotToken(e.target.value)} placeholder="bf_…" />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium">API URL (optional)</label>
            <Input value={apiUrl} onChange={(e) => setApiUrl(e.target.value)} placeholder="https://…/api" />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={submitting || !agentId.trim() || !botToken.trim()} onClick={submit}>
            {submitting ? "Saving…" : "Configure a bot"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
