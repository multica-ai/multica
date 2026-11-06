import { useEffect, useRef } from "react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { workspaceSlugForPath } from "@multica/core/paths";
import { useWorkspaceList } from "@multica/core/workspace";
import { useT } from "@multica/views/i18n";
import { useTabStore } from "@/stores/tab-store";
import {
  nextWorkspaceForNewWindow,
  openWorkspaceInNewWindow,
  otherWindowWorkspaces,
} from "./workspace-windows";

/**
 * Main-window side of multi-window workspaces: tells main which workspace
 * this window shows, opens paths other windows hand over, and answers
 * Cmd/Ctrl+Shift+N. Mounted in every main-window state (login included) so
 * a shortcut pressed before sign-in is consumed instead of replayed later.
 */
export function WorkspaceWindowBridge() {
  const { t } = useT("layout");
  const user = useAuthStore((s) => s.user);
  const { workspaces } = useWorkspaceList({ enabled: !!user });
  const activeWorkspaceSlug = useTabStore((s) => s.activeWorkspaceSlug);

  const latest = useRef({ workspaces, t });
  useEffect(() => {
    latest.current = { workspaces, t };
  }, [workspaces, t]);

  useEffect(() => {
    window.desktopAPI.reportWindowWorkspace?.(activeWorkspaceSlug);
  }, [activeWorkspaceSlug]);

  useEffect(
    () =>
      window.desktopAPI.onOpenWorkspacePath?.((path) => {
        const slug = workspaceSlugForPath(path);
        if (slug) useTabStore.getState().switchWorkspace(slug, path);
      }),
    [],
  );

  useEffect(
    () =>
      window.desktopAPI.onNewWindowShortcut?.(() => {
        const { workspaces: list, t: translate } = latest.current;
        if (list.length === 0) return;
        const slug = nextWorkspaceForNewWindow(
          list.map((workspace) => workspace.slug),
          useTabStore.getState().activeWorkspaceSlug,
          otherWindowWorkspaces(),
        );
        if (slug) {
          openWorkspaceInNewWindow(slug);
        } else {
          toast(translate(($) => $.rail.all_workspaces_open));
        }
      }),
    [],
  );

  return null;
}
