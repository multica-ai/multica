import { normalizeServerConfig, type ServerConfig } from "@/lib/server-url";
import { useAuthStore } from "./auth-store";
import { queryClient } from "./query-client";
import { useWorkspaceStore } from "./workspace-store";
import { persistServerConfig, useServerConfigStore } from "./server-config-store";
import { useChatDraftsStore } from "./stores/chat-drafts-store";
import { useViewedIssuesStore } from "./viewed-issues-store";
import { useLastViewedStore } from "./stores/last-viewed-store";
import { useFailedCommentsStore } from "./stores/failed-comments-store";
import { useMentionDraftStore } from "./stores/mention-draft-store";
import { useNewIssueDraftStore } from "./stores/new-issue-draft-store";
import { useNewProjectDraftStore } from "./stores/new-project-draft-store";
import { useReplyTargetStore } from "./stores/reply-target-store";
import { useChatSessionPickerStore } from "./stores/chat-session-picker-store";
import { useChatSelectStore } from "./chat-select-store";
import { useCommentSelectStore } from "./comment-select-store";

export async function changeServerConfig(input: ServerConfig): Promise<void> {
  const config = normalizeServerConfig(input);
  const state = useServerConfigStore.getState();
  if (state.switching) throw new Error("Server change already in progress");
  if (state.initialized && state.config.apiUrl === config.apiUrl &&
      state.config.webUrl === config.webUrl) return;

  useServerConfigStore.setState({ switching: true });
  try {
    // logout invalidates the epoch, aborts ALL requests (including writes),
    // closes WS and clears memory synchronously, before its Keychain await.
    // Delete the credential before persisting the new endpoint: an app killed
    // at any await can never restart with an old token and a new instance.
    const logout = useAuthStore.getState().logout();
    const cancel = queryClient.cancelQueries();
    queryClient.clear();
    useChatDraftsStore.setState({ drafts: {} });
    useViewedIssuesStore.setState({ byWorkspace: {} });
    useLastViewedStore.setState({ lastViewed: {} });
    useFailedCommentsStore.setState({ failed: {} });
    useMentionDraftStore.getState().clear();
    useNewIssueDraftStore.getState().reset();
    useNewProjectDraftStore.getState().reset();
    useReplyTargetStore.getState().clear();
    useChatSessionPickerStore.getState().reset();
    useChatSelectStore.getState().clear();
    useCommentSelectStore.getState().clear();
    await Promise.all([logout, cancel, useWorkspaceStore.getState().clear()]);
    await persistServerConfig(config);
  } finally {
    useServerConfigStore.setState({ switching: false });
  }
}
