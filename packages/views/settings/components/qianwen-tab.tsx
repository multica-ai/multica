"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  canAssignAgentToIssue,
  canEditAgent,
  useCurrentMember,
} from "@multica/core/permissions";
import {
  qianwenInstallationsOptions,
  useInstallQianwenPersonal,
  useMintQianwenPairingCode,
  useRevokeQianwenInstallation,
  useUnbindQianwenCurrentUser,
} from "@multica/core/qianwen";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
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
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useT } from "../../i18n";

export function QianwenTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const currentMember = useCurrentMember(wsId);
  const installationsQuery = useQuery(
    qianwenInstallationsOptions(wsId, currentMember.userId ?? ""),
  );
  const agentsQuery = useQuery(agentListOptions(wsId));
  const installMutation = useInstallQianwenPersonal(wsId);
  const pairingMutation = useMintQianwenPairingCode(wsId);
  const unbindMutation = useUnbindQianwenCurrentUser(wsId);
  const revokeMutation = useRevokeQianwenInstallation(wsId);

  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [oneTimeCredential, setOneTimeCredential] = useState<{
    connectionId: string;
    accessToken: string;
  } | null>(null);
  const [credentialSaved, setCredentialSaved] = useState(false);
  const [pairingCode, setPairingCode] = useState<{
    code: string;
    expiresAt: string;
  } | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<string | null>(null);

  const installations = useMemo(
    () => installationsQuery.data?.installations ?? [],
    [installationsQuery.data?.installations],
  );
  const activeInstalledAgentIds = useMemo(
    () =>
      new Set(
        installations
          .filter((installation) => installation.status === "active")
          .map((installation) => installation.agent_id),
      ),
    [installations],
  );
  const manageableAgents = useMemo(
    () =>
      (agentsQuery.data ?? []).filter(
        (agent) =>
          !agent.archived_at &&
          !activeInstalledAgentIds.has(agent.id) &&
          canEditAgent(agent, {
            userId: currentMember.userId,
            role: currentMember.role,
          }).allowed,
      ),
    [
      activeInstalledAgentIds,
      agentsQuery.data,
      currentMember.role,
      currentMember.userId,
    ],
  );
  const agentOptions = manageableAgents.map((agent) => ({
    value: agent.id,
    label: agent.name,
  }));
  const effectiveSelectedAgentId =
    selectedAgentId && manageableAgents.some((agent) => agent.id === selectedAgentId)
      ? selectedAgentId
      : manageableAgents.length === 1
        ? manageableAgents[0]?.id ?? null
        : null;
  const pairingSupported = installationsQuery.data?.pairing_supported === true;
  const agentNames = new Map(
    (agentsQuery.data ?? []).map((agent) => [agent.id, agent.name]),
  );
  const agentsById = new Map(
    (agentsQuery.data ?? []).map((agent) => [agent.id, agent]),
  );

  function reportActionFailure() {
    toast.error(t(($) => $.qianwen.action_failed));
  }

  async function handleInstall() {
    if (!effectiveSelectedAgentId || installMutation.isPending) return;
    try {
      const result = await installMutation.mutateAsync({
        agentId: effectiveSelectedAgentId,
      });
      setCredentialSaved(false);
      setOneTimeCredential({
        connectionId: result.connection_id,
        accessToken: result.access_token,
      });
    } catch {
      reportActionFailure();
    } finally {
      installMutation.reset();
    }
  }

  async function handleMintPairingCode(installationId: string) {
    if (!pairingSupported || pairingMutation.isPending) return;
    try {
      const result = await pairingMutation.mutateAsync({ installationId });
      setPairingCode({
        code: result.pairing_code,
        expiresAt: result.expires_at,
      });
    } catch {
      reportActionFailure();
    } finally {
      pairingMutation.reset();
    }
  }

  async function handleUnbind(installationId: string) {
    if (unbindMutation.isPending) return;
    try {
      await unbindMutation.mutateAsync({ installationId });
      toast.success(t(($) => $.qianwen.unlinked_success));
    } catch {
      reportActionFailure();
    }
  }

  async function handleRevoke() {
    if (!revokeTarget || revokeMutation.isPending) return;
    try {
      await revokeMutation.mutateAsync({ installationId: revokeTarget });
      setRevokeTarget(null);
      toast.success(t(($) => $.qianwen.revoked_success));
    } catch {
      reportActionFailure();
    }
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardContent className="space-y-4">
          <div className="space-y-1">
            <p className="text-body font-medium">
              {t(($) => $.qianwen.create_title)}
            </p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.qianwen.create_description)}
            </p>
          </div>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div className="min-w-0 flex-1 space-y-1.5">
              <Label htmlFor="qianwen-agent-select">
                {t(($) => $.qianwen.agent_label)}
              </Label>
              <Select
                items={agentOptions}
                value={effectiveSelectedAgentId}
                onValueChange={(value) => setSelectedAgentId(value)}
              >
                <SelectTrigger
                  id="qianwen-agent-select"
                  className="w-full"
                  disabled={
                    !pairingSupported ||
                    installMutation.isPending ||
                    manageableAgents.length === 0
                  }
                >
                  <SelectValue placeholder={t(($) => $.qianwen.agent_placeholder)} />
                </SelectTrigger>
                <SelectContent>
                  {manageableAgents.map((agent) => (
                    <SelectItem key={agent.id} value={agent.id}>
                      {agent.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <Button
              type="button"
              onClick={() => void handleInstall()}
              disabled={
                !pairingSupported ||
                !effectiveSelectedAgentId ||
                installMutation.isPending
              }
            >
              {installMutation.isPending
                ? t(($) => $.qianwen.creating)
                : t(($) => $.qianwen.create_action)}
            </Button>
          </div>
          {!pairingSupported ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.qianwen.pairing_unavailable)}
            </p>
          ) : null}
        </CardContent>
      </Card>

      {installations.length > 0 ? (
        <Card>
          <CardContent className="divide-y divide-surface-border">
            {installations.map((installation) => {
              const agent = agentsById.get(installation.agent_id);
              const canRevoke =
                agent !== undefined &&
                canEditAgent(agent, {
                  userId: currentMember.userId,
                  role: currentMember.role,
                }).allowed;
              const canPair =
                agent !== undefined &&
                canAssignAgentToIssue(agent, {
                  userId: currentMember.userId,
                  role: currentMember.role,
                }).allowed;
              let connectionStatus: string;
              switch (installation.status) {
                case "active":
                  connectionStatus = t(($) => $.qianwen.connection_active);
                  break;
                case "revoked":
                  connectionStatus = t(($) => $.qianwen.connection_revoked);
                  break;
                default:
                  connectionStatus = t(($) => $.qianwen.connection_unknown);
                  break;
              }

              const identityStatus =
                installation.current_user_bound === true
                  ? t(($) => $.qianwen.identity_linked)
                  : installation.current_user_bound === false
                    ? t(($) => $.qianwen.identity_unlinked)
                    : t(($) => $.qianwen.identity_unknown);

              return (
                <div
                  key={installation.id}
                  role="group"
                  aria-label={installation.connection_id}
                  className="space-y-2 py-4 first:pt-0 last:pb-0"
                >
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="truncate text-body font-medium">
                        {agentNames.get(installation.agent_id) ??
                          t(($) => $.qianwen.unknown_agent)}
                      </p>
                      <p className="break-all text-micro text-muted-foreground">
                        {installation.connection_id}
                      </p>
                    </div>
                    <div className="flex flex-wrap gap-2 text-caption">
                      <span className="rounded-md bg-muted px-2 py-1">
                        {connectionStatus}
                      </span>
                      <span className="rounded-md bg-muted px-2 py-1">
                        {identityStatus}
                      </span>
                    </div>
                  </div>
                  {installation.status === "active" ? (
                    <div className="flex flex-wrap gap-2">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={installationsQuery.isFetching}
                        onClick={() => void installationsQuery.refetch()}
                      >
                        {installationsQuery.isFetching
                          ? t(($) => $.qianwen.refreshing_status)
                          : t(($) => $.qianwen.refresh_status)}
                      </Button>
                      {installation.current_user_bound === true ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={unbindMutation.isPending}
                          onClick={() => void handleUnbind(installation.id)}
                        >
                          {t(($) => $.qianwen.unbind_me)}
                        </Button>
                      ) : installation.current_user_bound === false && canPair ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={!pairingSupported || pairingMutation.isPending}
                          onClick={() => void handleMintPairingCode(installation.id)}
                        >
                          {t(($) => $.qianwen.generate_pairing_code)}
                        </Button>
                      ) : null}
                      {canRevoke ? (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={revokeMutation.isPending}
                          onClick={() => setRevokeTarget(installation.id)}
                        >
                          {t(($) => $.qianwen.revoke_action)}
                        </Button>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </CardContent>
        </Card>
      ) : null}

      <AlertDialog
        open={revokeTarget !== null}
        onOpenChange={(open) => {
          if (!open && !revokeMutation.isPending) setRevokeTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.qianwen.revoke_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.qianwen.revoke_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revokeMutation.isPending}>
              {t(($) => $.qianwen.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={revokeMutation.isPending}
              onClick={() => void handleRevoke()}
            >
              {t(($) => $.qianwen.revoke_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog
        open={pairingCode !== null}
        onOpenChange={(open) => {
          if (!open) setPairingCode(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.qianwen.pairing_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.qianwen.pairing_description)}
            </DialogDescription>
          </DialogHeader>
          {pairingCode ? (
            <div className="space-y-2">
              <code className="block rounded-lg bg-muted p-4 text-center text-title tracking-[0.25em]">
                {pairingCode.code}
              </code>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.qianwen.pairing_expires)}
              </p>
              <time
                dateTime={pairingCode.expiresAt}
                className="block text-micro text-muted-foreground"
              >
                {new Date(pairingCode.expiresAt).toLocaleString()}
              </time>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog
        open={oneTimeCredential !== null}
        onOpenChange={() => undefined}
        disablePointerDismissal
      >
        <DialogContent showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>{t(($) => $.qianwen.secret_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.qianwen.secret_description)}
            </DialogDescription>
          </DialogHeader>
          {oneTimeCredential ? (
            <div className="space-y-4">
              <div className="space-y-1">
                <p className="text-caption font-medium">
                  {t(($) => $.qianwen.connection_id_label)}
                </p>
                <code className="block break-all rounded-lg bg-muted p-3 text-caption">
                  {oneTimeCredential.connectionId}
                </code>
              </div>
              <div className="space-y-1">
                <p className="text-caption font-medium">
                  {t(($) => $.qianwen.qws_label)}
                </p>
                <code className="block break-all rounded-lg bg-muted p-3 text-caption">
                  {oneTimeCredential.accessToken}
                </code>
              </div>
              <div className="flex items-start gap-2">
                <Checkbox
                  id="qianwen-secret-saved"
                  checked={credentialSaved}
                  onCheckedChange={(checked) => setCredentialSaved(checked === true)}
                />
                <Label htmlFor="qianwen-secret-saved" className="text-caption leading-5">
                  {t(($) => $.qianwen.secret_saved_acknowledgement)}
                </Label>
              </div>
            </div>
          ) : null}
          <DialogFooter>
            <Button
              type="button"
              disabled={!credentialSaved}
              onClick={() => {
                if (!credentialSaved) return;
                setOneTimeCredential(null);
                setCredentialSaved(false);
              }}
            >
              {t(($) => $.qianwen.secret_saved_action)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
