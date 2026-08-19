"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
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
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  dingtalkGroupRoutesOptions,
  dingtalkInstallationsOptions,
  dingtalkKeys,
} from "@multica/core/dingtalk";
import { api } from "@multica/core/api";
import type {
  DingTalkGroupRoute,
  DingTalkInstallation,
} from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

// formatInstalledAt renders the install timestamp defensively: the schema
// defaults installed_at to "" and the backend can emit a zero-value timestamp
// (0001-01-01T…) for a never-set time, either of which would otherwise surface
// as "Invalid Date" or a year-1 date. Fall back to a neutral placeholder.
function formatInstalledAt(value: string): string {
  const t = Date.parse(value);
  if (!value || Number.isNaN(t) || t <= 0) return "—";
  return new Date(t).toLocaleString();
}

// DingTalkTab is the workspace settings panel for DingTalk robot installations.
// Listing is member-visible; the disconnect action is admin-only (the backend
// enforces it; the UI hides the button for non-admins to match).
//
// New Agent-scoped binding is retired. This independent workspace surface
// lists, routes, and disconnects retained installations without creating new ones.
export function DingTalkTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const { data, isLoading } = useQuery({
    ...dingtalkInstallationsOptions(wsId),
  });
  const installations = data?.installations ?? [];
  const configured = data?.configured === true;
  const groupRoutingSupported = data?.group_routing_supported === true;
  const hasActiveInstallation = installations.some(
    (installation) => installation.status === "active",
  );
  const {
    data: groupRouteData,
    isLoading: routesLoading,
    isError: routesError,
    isFetching: routesFetching,
    refetch: retryGroupRoutes,
  } = useQuery({
    ...dingtalkGroupRoutesOptions(wsId),
    enabled:
      configured && groupRoutingSupported && hasActiveInstallation && !!wsId,
  });
  const groupRoutes = groupRouteData?.routes ?? [];
  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);
  const [disconnecting, setDisconnecting] = useState(false);

  async function handleDisconnect() {
    if (!disconnectTarget || disconnecting) return;
    setDisconnecting(true);
    try {
      await api.deleteDingTalkInstallation(wsId, disconnectTarget);
      await qc.invalidateQueries({
        queryKey: dingtalkKeys.installations(wsId),
      });
      toast.success(t(($) => $.dingtalk.toast_disconnected));
      setDisconnectTarget(null);
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.dingtalk.toast_disconnect_failed),
      );
    } finally {
      setDisconnecting(false);
    }
  }

  return (
    <div className="space-y-8">
      {!configured ? (
        <Card>
          <CardContent className="space-y-2">
            <p className="text-body font-medium">
              {t(($) => $.dingtalk.not_enabled_title)}
            </p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.dingtalk.not_enabled_description_prefix)}{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-micro">
                MULTICA_DINGTALK_SECRET_KEY
              </code>{" "}
              {t(($) => $.dingtalk.not_enabled_description_suffix)}{" "}
              {t(($) => $.dingtalk.not_enabled_self_host_hint)}
            </p>
          </CardContent>
        </Card>
      ) : (
        <section className="space-y-3">
          <h2 className="text-body font-semibold">
            {t(($) => $.dingtalk.connected_bots)}
          </h2>
          {isLoading ? (
            <Card>
              <CardContent>
                <p className="text-body text-muted-foreground">
                  {t(($) => $.dingtalk.loading)}
                </p>
              </CardContent>
            </Card>
          ) : installations.length === 0 ? (
            <Card>
              <CardContent className="space-y-2">
                <p className="text-body font-medium">
                  {t(($) => $.dingtalk.empty_title)}
                </p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.dingtalk.empty_description_prefix)}
                </p>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="divide-y">
                {installations.map((inst) => (
                  <InstallationRow
                    key={inst.id}
                    installation={inst}
                    canManage={canManage}
                    onDisconnect={() => setDisconnectTarget(inst.id)}
                  />
                ))}
              </CardContent>
            </Card>
          )}
        </section>
      )}

      {configured && groupRoutingSupported && hasActiveInstallation && (
        <section className="space-y-3">
          <div className="space-y-1">
            <h2 className="text-body font-semibold">
              {t(($) => $.dingtalk.group_routes_title)}
            </h2>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.dingtalk.group_routes_description)}
            </p>
          </div>
          {routesLoading ? (
            <Card>
              <CardContent>
                <p className="text-body text-muted-foreground">
                  {t(($) => $.dingtalk.loading)}
                </p>
              </CardContent>
            </Card>
          ) : routesError ? (
            <Card>
              <CardContent className="space-y-3" role="alert">
                <div className="space-y-1">
                  <p className="text-body font-medium">
                    {t(($) => $.dingtalk.group_routes_error_title)}
                  </p>
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.dingtalk.group_routes_error_description)}
                  </p>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={routesFetching}
                  onClick={() => void retryGroupRoutes()}
                >
                  {t(($) => $.dingtalk.group_routes_retry)}
                </Button>
              </CardContent>
            </Card>
          ) : groupRoutes.length === 0 ? (
            <Card>
              <CardContent className="space-y-1">
                <p className="text-body font-medium">
                  {t(($) => $.dingtalk.group_routes_empty_title)}
                </p>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.dingtalk.group_routes_empty_description)}
                </p>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="divide-y">
                {groupRoutes.map((route) => (
                  <GroupRouteRow key={route.id} route={route} />
                ))}
              </CardContent>
            </Card>
          )}
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
            <AlertDialogTitle>
              {t(($) => $.dingtalk.disconnect_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.dingtalk.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={disconnecting}>
              {t(($) => $.dingtalk.disconnect_confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDisconnect}
              disabled={disconnecting}
            >
              {disconnecting
                ? t(($) => $.dingtalk.disconnecting)
                : t(($) => $.dingtalk.disconnect)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function GroupRouteRow({
  route,
}: {
  route: DingTalkGroupRoute;
}) {
  const { t } = useT("settings");
  const { getAgentName } = useActorName();
  const title = route.conversation_title || route.conversation_id;
  const selectedLabel =
    getAgentName(route.agent_id) ||
    t(($) => $.dingtalk.group_routes_unknown_agent);

  return (
    <div className="flex flex-col gap-3 py-3 first:pt-0 last:pb-0 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 space-y-1">
        <p className="truncate text-body font-medium">{title}</p>
        <p className="truncate font-mono text-micro text-muted-foreground">
          {route.conversation_id}
        </p>
      </div>
      <p className="text-caption text-muted-foreground">{selectedLabel}</p>
    </div>
  );
}

function InstallationRow({
  installation,
  canManage,
  onDisconnect,
}: {
  installation: DingTalkInstallation;
  canManage: boolean;
  onDisconnect: () => void;
}) {
  const { t } = useT("settings");
  const { getAgentName } = useActorName();
  const isActive = installation.status === "active";
  const agentName = getAgentName(installation.agent_id);
  return (
    <div className="flex items-start justify-between gap-4 py-3 first:pt-0 last:pb-0">
      <div className="flex items-start gap-3">
        <ActorAvatar
          actorType="agent"
          actorId={installation.agent_id}
          size="lg"
          enableHoverCard
          profileLink
        />
        <div className="space-y-1">
          <p className="text-body font-medium">
            {agentName}
            {!isActive && (
              <span className="ml-2 rounded bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                {t(($) => $.dingtalk.revoked_badge)}
              </span>
            )}
          </p>
          <p className="text-micro text-muted-foreground">
            {t(($) => $.dingtalk.installed_at_label, {
              when: formatInstalledAt(installation.installed_at),
            })}
          </p>
        </div>
      </div>
      {canManage && isActive && (
        <Button variant="outline" size="sm" onClick={onDisconnect}>
          <Trash2 className="h-3 w-3" />
          {t(($) => $.dingtalk.disconnect)}
        </Button>
      )}
    </div>
  );
}
