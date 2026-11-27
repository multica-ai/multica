"use client";

import { useEffect, useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWS } from "@multica/core/realtime";
import { getCurrentWsId, subscribeToCurrentSlug } from "@multica/core/platform";
import { closeGraphScope } from "@multica/core/trajectory/graph-web";
import { useT } from "../../../../i18n";
import { GraphTrajectoryView } from "./graph-trajectory-view";
import type { TrajectoryPageProps } from "./trajectory-entry";

export default function TrajectoryActivePage(props: TrajectoryPageProps) {
  const { onScopeChange } = props;
  const { t } = useT("issues");
  const accountId = useAuthStore(state => state.user?.id);
  const workspaceId = useWorkspaceId();
  const client = useQueryClient();
  const { subscribe, onReconnect } = useWS();
  const realtime = useMemo(() => ({ subscribe, onReconnect }), [subscribe, onReconnect]);
  const scope = useMemo(() => accountId ? { accountId, workspaceId } : null, [accountId, workspaceId]);
  useEffect(() => {
    if (!scope) return;
    const authCleanup = useAuthStore.subscribe(state => {
      if (state.user?.id !== scope.accountId || state.status === "unauthenticated") { closeGraphScope(client, scope); onScopeChange(); }
    });
    const workspaceCleanup = subscribeToCurrentSlug(() => {
      if (getCurrentWsId() !== scope.workspaceId) { closeGraphScope(client, scope); onScopeChange(); }
    });
    return () => { authCleanup(); workspaceCleanup(); };
  }, [client, scope, onScopeChange]);
  return scope ? <GraphTrajectoryView {...props} scope={scope} realtime={realtime} /> : <p role="alert">{t($ => $.trajectory.unavailable)}</p>;
}
