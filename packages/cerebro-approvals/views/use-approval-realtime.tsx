"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useWSEvent } from "@multica/core/realtime";
import { approvalKeys } from "../core/queries";
import { useApprovalExperienceEnabled } from "../core/availability";

export interface ApprovalRealtimeProps {
  wsId: string;
  onCreated?: () => void;
}

function EnabledApprovalRealtime({ wsId, onCreated }: ApprovalRealtimeProps) {
  const queryClient = useQueryClient();
  const invalidate = useCallback(() => {
    if (wsId) {
      queryClient.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
    }
  }, [queryClient, wsId]);

  useWSEvent(
    "approval:created",
    useCallback(() => {
      onCreated?.();
      invalidate();
    }, [invalidate, onCreated]),
  );
  useWSEvent("approval:decided", invalidate);
  useWSEvent("approval:delegated", invalidate);
  useWSEvent("approval:expired", invalidate);

  return null;
}

export function ApprovalRealtime(props: ApprovalRealtimeProps) {
  const enabled = useApprovalExperienceEnabled();
  if (!enabled) return null;
  return <EnabledApprovalRealtime {...props} />;
}
