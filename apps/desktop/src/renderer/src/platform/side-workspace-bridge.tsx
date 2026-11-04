import { useEffect, useRef } from "react";
import { useAuthStore } from "@multica/core/auth";
import { getCurrentWsId, getCurrentSlug } from "@multica/core/platform";
import { paths } from "@multica/core/paths";
import { useWorkspaceList } from "@multica/core/workspace";
import { useTabStore } from "../stores/tab-store";
import { conversationService } from "./conversation-services";
import { applySideCommand, hasSideWorkspaceView, prepareSideTab, readSideLayout, sideLayoutKey, sideSnapshot, syncSideWorkspace } from "../pages/local-side-workspace-sync";
import { sideWorkspaceControlSchema } from "../../../shared/side-workspace";
import { localV2Transport } from "./local-v2-transport";

/** Restores snapshots even when Desktop starts on a page other than conversations. */
export function DesktopSideWorkspaceBridge() {
  const userId = useAuthStore((state) => state.user?.id);
  const authStatus = useAuthStore((state) => state.status);
  const slug = useTabStore((state) => state.activeWorkspaceSlug);
  const { workspaces, ready, unavailable } = useWorkspaceList();
  const workspaceId = workspaces?.find((workspace) => workspace.slug === slug)?.id;
  const enabled = authStatus === "authenticated" && ready && !unavailable;
  const active = useRef({ userId, workspaceId, enabled }); active.current = { userId, workspaceId, enabled };
  useEffect(() => {
    if (!enabled || !userId || !workspaceId) return;
    const scope = { userId, workspaceId }, transport = localV2Transport();
    const fullSync = () => {
      if (hasSideWorkspaceView(scope)) return;
      const prefix = sideLayoutKey(scope, "");
      for (let index = 0; index < localStorage.length; index++) {
        const key = localStorage.key(index);
        if (key?.startsWith(prefix)) void syncSideWorkspace(transport, scope, sideSnapshot(key.slice(prefix.length), readSideLayout(key), false)).catch(() => {});
      }
    };
    fullSync();
    // A restarted daemon has lost the layouts.
    return transport.onConnection((changed, connected) => { if (connected && changed.userId === userId && changed.workspaceId === workspaceId) fullSync(); });
  }, [userId, workspaceId, enabled]);
  useEffect(() => window.desktopAPI.sideWorkspace?.onCommand((command) => {
    const request = sideWorkspaceControlSchema.parse(command.request);
    const deviceId = request.deviceId ?? "local";
    if (hasSideWorkspaceView(request.scope, deviceId)) return;
    void (async () => {
      const check = () => {
        if (!active.current.enabled || active.current.userId !== request.scope.userId || getCurrentWsId() !== request.scope.workspaceId) throw new Error("Select the originating account and workspace before controlling Side tabs");
      };
      check();
      const transport = localV2Transport(deviceId);
      const opened = await prepareSideTab(request, transport); check();
      const key = sideLayoutKey(request.scope, request.conversationId, deviceId);
      const { layout, tabId } = applySideCommand(readSideLayout(key), request, opened);
      localStorage.setItem(key, JSON.stringify(layout));
      const snapshot = sideSnapshot(request.conversationId, layout, false);
      await syncSideWorkspace(transport, request.scope, snapshot); check();
      if (request.op === "select" || (request.op === "open" && request.select !== false)) {
        const slug = getCurrentSlug(); if (!slug) throw new Error("No selected workspace");
        const source = (await conversationService(transport, request.scope).snapshot(request.conversationId)).conversation; check();
        const flow = deviceId === "local" ? paths.workspace(slug).conversationFlow() : paths.workspace(slug).conversationFlowDevice(deviceId);
        const path = `${flow}?${new URLSearchParams({ session: source.conversation_id, provider: source.provider })}`;
        useTabStore.getState().openTab(path, "", { activate: true });
        // openTab deduplicates by pathname and intentionally retains the old query.
        useTabStore.getState().navigateActiveSession(path, { replace: true });
        await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      }
      await window.desktopAPI.sideWorkspace.ack({ id: command.id, snapshot, tabId });
    })().catch((error: unknown) => { void window.desktopAPI.sideWorkspace.ack({ id: command.id, error: error instanceof Error ? error.message : String(error) }); });
  }), []);
  return null;
}
