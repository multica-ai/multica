"use client";

import { useCallback, useMemo, useRef } from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useCurrentWorkspace } from "@multica/core/paths";
import { quickActionListOptions } from "@multica/core/quick-actions";

/**
 * Supplies the comment composer's `/` menu with this workspace's quick actions
 * (MUL-5465).
 *
 * Picking one INSERTS the rendered body rather than running it — the composer
 * path exists precisely for the "this time is different" case, so the user
 * gets to edit and then send with the normal shortcut. Running without review
 * is what the sidebar button is for.
 *
 * The menu offers the same catalog the sidebar shows — no permission
 * pre-filtering. A pick the user cannot run fails at the render endpoint and
 * simply inserts nothing, matching how the sidebar reports a refusal.
 *
 * Reads workspace identity through `useCurrentWorkspace` (nullable) rather
 * than `useWorkspaceId` (throws): the comment composer also mounts outside a
 * workspace route, and an enhancement like this must degrade to "no quick
 * actions in the menu", never take the composer down with it.
 */
export function useQuickActionMenu(issueId: string) {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const { data = [] } = useQuery({
    ...quickActionListOptions(wsId),
    enabled: wsId !== "",
  });

  // The extension reads through a getter on every keystroke; a ref keeps that
  // read cheap and current without re-creating the editor.
  const actionsRef = useRef<{ id: string; name: string; description?: string }[]>([]);
  actionsRef.current = useMemo(
    () =>
      data
        .filter((a) => a.status === "active")
        .map((a) => ({ id: a.id, name: a.name, description: a.description || undefined })),
    [data],
  );

  const getQuickActions = useCallback(() => actionsRef.current, []);
  const renderQuickAction = useCallback(
    (quickActionId: string) => api.renderQuickAction(issueId, quickActionId),
    [issueId],
  );

  // The extension runs outside React's tree, so it reports failures back here
  // rather than rendering anything itself. Without this a failed pick left the
  // command text sitting in the composer with no explanation.
  const onRenderError = useCallback((error: unknown) => {
    toast.error(error instanceof Error ? error.message : String(error));
  }, []);

  return useMemo(
    () => ({ getQuickActions, renderQuickAction, onRenderError }),
    [getQuickActions, renderQuickAction, onRenderError],
  );
}
