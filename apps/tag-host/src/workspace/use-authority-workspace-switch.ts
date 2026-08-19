import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  completeAuthorityWorkspaceSwitch,
  tagAuthorityClient,
  type AuthorityWorkspace,
} from "@multica/core/tag-authority";
import { useModalStore } from "@multica/core/modals";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useWS } from "@multica/core/realtime";
import { toTagHostPath } from "@/platform/paths";

export function useAuthorityWorkspaceSwitch() {
  const queryClient = useQueryClient();
  const { disconnectCurrent } = useWS();
  const [switchingWorkspaceId, setSwitchingWorkspaceId] = useState<
    string | null
  >(null);
  const [switchError, setSwitchError] = useState("");

  const switchTo = async (
    workspace: Pick<AuthorityWorkspace, "id" | "slug">,
    destination: string,
  ) => {
    setSwitchingWorkspaceId(workspace.id);
    setSwitchError("");
    try {
      await completeAuthorityWorkspaceSwitch({
        client: tagAuthorityClient,
        workspaceId: workspace.id,
        destination: toTagHostPath(destination),
        queryClient,
        disconnectRealtime: disconnectCurrent,
        clearClientSelection: () => {
          useModalStore.getState().close();
          setCurrentWorkspace(null, null);
        },
        navigate: (href) => window.location.assign(href),
      });
    } catch {
      setSwitchError("Workspace switch failed. Retry safely.");
      setSwitchingWorkspaceId(null);
    }
  };

  return { switchTo, switchingWorkspaceId, switchError };
}
