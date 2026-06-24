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
  /**
   * When true, the buttons are always rendered regardless of the current
   * node-run status or the user's permission. Clicking a button that cannot
   * be performed toasts an error instead of hiding the affordance.
   */
  alwaysShow?: boolean;
}

export function NodeRunControlActions({
  nodeRun,
  workflowId,
  runId,
  wsId,
  size = "sm",
  alwaysShow = false,
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
  const isWorking = status === "working";
  const isBlocked = status === "blocked";

  const anyControlPending =
    takeoverMutation.isPending || handbackMutation.isPending || finalizeMutation.isPending;

  const buttonClass = size === "sm" ? "h-7 text-xs" : "h-8 text-sm";
  const iconClass = "h-3.5 w-3.5";

  const handleTakeover = () => {
    if (!controlDecision.allowed) {
      toast.error(t(($) => $.node_run.no_control_permission));
      return;
    }
    if (!isWorking) {
      toast.error(t(($) => $.node_run.take_over_wrong_status));
      return;
    }
    takeoverMutation.mutate(controlVars, {
      onSuccess: () => toast.success(t(($) => $.node_run.toast_takeover_success)),
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : t(($) => $.node_run.toast_takeover_failed)),
    });
  };

  const handleHandback = () => {
    if (!controlDecision.allowed) {
      toast.error(t(($) => $.node_run.no_control_permission));
      return;
    }
    if (!isBlocked) {
      toast.error(t(($) => $.node_run.hand_back_wrong_status));
      return;
    }
    handbackMutation.mutate(controlVars, {
      onSuccess: () => toast.success(t(($) => $.node_run.toast_handback_success)),
      onError: (err) =>
        toast.error(err instanceof Error ? err.message : t(($) => $.node_run.toast_handback_failed)),
    });
  };

  const handleFinalize = (approved: boolean) => {
    if (!controlDecision.allowed) {
      toast.error(t(($) => $.node_run.no_control_permission));
      return;
    }
    if (!isBlocked) {
      toast.error(t(($) => $.node_run.finalize_wrong_status));
      return;
    }
    finalizeMutation.mutate(
      { ...controlVars, approved },
      {
        onSuccess: () =>
          toast.success(
            approved ? t(($) => $.node_run.toast_finalize_approved) : t(($) => $.node_run.toast_finalize_rejected),
          ),
        onError: (err) =>
          toast.error(err instanceof Error ? err.message : t(($) => $.node_run.toast_finalize_failed)),
      },
    );
  };

  const controlVars = {
    nodeRunId: nodeRun.id,
    sessionId: nodeRun.session_id,
    workflowId,
    runId,
  };

  // Compact mode: only render when the actions are meaningful, like on the
  // workflow editor/run page.
  if (!alwaysShow) {
    const canTakeover = isWorking && controlDecision.allowed;
    const canHandbackOrFinalize = isBlocked && controlDecision.allowed;
    if (!canTakeover && !canHandbackOrFinalize) {
      return null;
    }
    return (
      <div className="flex items-center gap-1.5 flex-wrap">
        {canTakeover && (
          <Button
            size={size}
            variant="outline"
            className={buttonClass}
            onClick={handleTakeover}
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
              onClick={handleHandback}
              disabled={anyControlPending}
            >
              <Play className={iconClass + " mr-1"} />
              {handbackMutation.isPending ? t(($) => $.node_run.handing_back) : t(($) => $.node_run.hand_back)}
            </Button>
            <Button
              size={size}
              className={buttonClass}
              onClick={() => handleFinalize(true)}
              disabled={anyControlPending}
            >
              <CheckCircle className={iconClass + " mr-1"} />
              {finalizeMutation.isPending ? t(($) => $.node_run.finalizing) : t(($) => $.node_run.finalize_approve)}
            </Button>
            <Button
              size={size}
              variant="outline"
              className={buttonClass}
              onClick={() => handleFinalize(false)}
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

  // Always-visible mode: used on issue detail pages. Buttons stay on screen so
  // users can discover them; invalid clicks surface an explanatory toast.
  return (
    <div className="flex items-center gap-1.5 flex-wrap">
      <Button
        size={size}
        variant="outline"
        className={buttonClass}
        onClick={handleTakeover}
        disabled={anyControlPending}
      >
        <Hand className={iconClass + " mr-1"} />
        {takeoverMutation.isPending ? t(($) => $.node_run.taking_over) : t(($) => $.node_run.take_over)}
      </Button>
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
        onClick={handleHandback}
        disabled={anyControlPending}
      >
        <Play className={iconClass + " mr-1"} />
        {handbackMutation.isPending ? t(($) => $.node_run.handing_back) : t(($) => $.node_run.hand_back)}
      </Button>
      <Button
        size={size}
        className={buttonClass}
        onClick={() => handleFinalize(true)}
        disabled={anyControlPending}
      >
        <CheckCircle className={iconClass + " mr-1"} />
        {finalizeMutation.isPending ? t(($) => $.node_run.finalizing) : t(($) => $.node_run.finalize_approve)}
      </Button>
      <Button
        size={size}
        variant="outline"
        className={buttonClass}
        onClick={() => handleFinalize(false)}
        disabled={anyControlPending}
      >
        <XCircle className={iconClass + " mr-1"} />
        {finalizeMutation.isPending ? t(($) => $.node_run.finalizing) : t(($) => $.node_run.finalize_reject)}
      </Button>
    </div>
  );
}
