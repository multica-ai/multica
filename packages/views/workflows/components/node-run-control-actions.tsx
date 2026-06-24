"use client";

import { Hand, Play, CheckCircle, XCircle, MessageSquare } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { WorkflowNodeRun } from "@multica/core/types";
import {
  useSessionPermission,
  useTakeoverNodeRun,
  useHandbackNodeRun,
  useFinalizeNodeRun,
} from "@multica/core/workflows/queries";
import { myRuntimePermissionOptions } from "@multica/core/runtimes/queries";
import { useNodeRunControlPermission } from "@multica/core/permissions";
import { useChatStore } from "@multica/core/chat";
import {
  isEmbeddedInCostrict,
  postCostrictNavigateToSession,
} from "@multica/core/platform";
import { useT } from "../../i18n";

interface NodeRunControlActionsProps {
  nodeRun: WorkflowNodeRun;
  workflowId?: string;
  runId?: string;
  wsId: string;
  size?: "sm" | "default";
}

export function NodeRunControlActions({
  nodeRun,
  workflowId,
  runId,
  wsId,
  size = "sm",
}: NodeRunControlActionsProps) {
  const { t } = useT("workflows");

  const takeoverMutation = useTakeoverNodeRun(wsId);
  const handbackMutation = useHandbackNodeRun(wsId);
  const finalizeMutation = useFinalizeNodeRun(wsId);
  const setChatSession = useChatStore((s) => s.setActiveSession);
  const setChatOpen = useChatStore((s) => s.setOpen);

  const handleOpenSession = () => {
    const sessionId = nodeRun.session_id;
    if (!sessionId) {
      toast.error(t(($) => $.node_run.open_session_missing));
      return;
    }
    if (isEmbeddedInCostrict()) {
      postCostrictNavigateToSession({ sessionId });
      return;
    }
    setChatSession(sessionId);
    setChatOpen(true);
  };

  const { data: sessionPerm } = useSessionPermission(nodeRun.session_id);
  const { data: runtimePerm } = useQuery({
    ...myRuntimePermissionOptions(nodeRun.runtime_id ?? ""),
    enabled: !!nodeRun.runtime_id && !nodeRun.session_id,
  });

  const canControl = nodeRun.session_id
    ? sessionPerm?.can_control
    : runtimePerm?.can_control;
  const controlDecision = useNodeRunControlPermission(!!canControl, wsId);

  const status = nodeRun.status;
  const canTakeover = status === "working" && controlDecision.allowed;
  const canHandbackOrFinalize = status === "blocked" && controlDecision.allowed;

  const controlVars = {
    nodeRunId: nodeRun.id,
    sessionId: nodeRun.session_id,
    workflowId,
    runId,
  };

  const anyControlPending =
    takeoverMutation.isPending || handbackMutation.isPending || finalizeMutation.isPending;

  if (!canTakeover && !canHandbackOrFinalize) {
    return null;
  }

  const buttonClass = size === "sm" ? "h-7 text-xs" : "h-8 text-sm";
  const iconClass = "h-3.5 w-3.5";

  return (
    <div className="flex items-center gap-1.5 flex-wrap">
      {canTakeover && (
        <Button
          size={size}
          variant="outline"
          className={buttonClass}
          onClick={() =>
            takeoverMutation.mutate(controlVars, {
              onSuccess: () => toast.success(t(($) => $.node_run.toast_takeover_success)),
              onError: (err) =>
                toast.error(err instanceof Error ? err.message : t(($) => $.node_run.toast_takeover_failed)),
            })
          }
          disabled={anyControlPending}
        >
          <Hand className={iconClass + " mr-1"} />
          {takeoverMutation.isPending ? t(($) => $.node_run.taking_over) : t(($) => $.node_run.take_over)}
        </Button>
      )}
      {canHandbackOrFinalize && (
        <>
          <Button
            size={size}
            variant="outline"
            className={buttonClass}
            onClick={handleOpenSession}
          >
            <MessageSquare className={iconClass + " mr-1"} />
            {t(($) => $.node_run.open_session)}
          </Button>
          <Button
            size={size}
            variant="outline"
            className={buttonClass}
            onClick={() =>
              handbackMutation.mutate(controlVars, {
                onSuccess: () => toast.success(t(($) => $.node_run.toast_handback_success)),
                onError: (err) =>
                  toast.error(err instanceof Error ? err.message : t(($) => $.node_run.toast_handback_failed)),
              })
            }
            disabled={anyControlPending}
          >
            <Play className={iconClass + " mr-1"} />
            {handbackMutation.isPending ? t(($) => $.node_run.handing_back) : t(($) => $.node_run.hand_back)}
          </Button>
          <Button
            size={size}
            className={buttonClass}
            onClick={() =>
              finalizeMutation.mutate(
                { ...controlVars, approved: true },
                {
                  onSuccess: () => toast.success(t(($) => $.node_run.toast_finalize_approved)),
                  onError: (err) =>
                    toast.error(err instanceof Error ? err.message : t(($) => $.node_run.toast_finalize_failed)),
                },
              )
            }
            disabled={anyControlPending}
          >
            <CheckCircle className={iconClass + " mr-1"} />
            {finalizeMutation.isPending ? t(($) => $.node_run.finalizing) : t(($) => $.node_run.finalize_approve)}
          </Button>
          <Button
            size={size}
            variant="outline"
            className={buttonClass}
            onClick={() =>
              finalizeMutation.mutate(
                { ...controlVars, approved: false },
                {
                  onSuccess: () => toast.success(t(($) => $.node_run.toast_finalize_rejected)),
                  onError: (err) =>
                    toast.error(err instanceof Error ? err.message : t(($) => $.node_run.toast_finalize_failed)),
                },
              )
            }
            disabled={anyControlPending}
          >
            <XCircle className={iconClass + " mr-1"} />
            {finalizeMutation.isPending ? t(($) => $.node_run.finalizing) : t(($) => $.node_run.finalize_reject)}
          </Button>
        </>
      )}
    </div>
  );
}
