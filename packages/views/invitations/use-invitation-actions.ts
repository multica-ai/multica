import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { paths, useHasOnboarded } from "@multica/core/paths";
import type { Invitation } from "@multica/core/types";
import {
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { useNavigation } from "../navigation";

/**
 * Accept / decline for a pending invitation. The invitations page is the only
 * surface that answers invitations; the workspace switcher links to it.
 */
export function useInvitationActions() {
  const queryClient = useQueryClient();
  const { push } = useNavigation();
  const hasOnboarded = useHasOnboarded();

  const accept = useMutation({
    mutationFn: async (invitation: Invitation) => {
      await api.acceptInvitation(invitation.id);
      // The accept transaction already sets onboarded_at; this records the
      // completion path and brings the local user out of the
      // un-onboarded state before guards look at it. Losing the completion
      // path is fine, but the local user must be onboarded before entering
      // the workspace or its layout bounces back to onboarding.
      if (!hasOnboarded) {
        const { setUser, refreshMe } = useAuthStore.getState();
        try {
          setUser(
            await api.markOnboardingComplete({
              completion_path: "invite_accept",
              workspace_id: invitation.workspace_id,
            }),
          );
        } catch {
          await refreshMe();
        }
      }
      // staleTime: 0 forces a real fetch — the joined workspace has to be in
      // the list before its slug can be resolved.
      return queryClient.fetchQuery({ ...workspaceListOptions(), staleTime: 0 });
    },
    // Land inside the joined workspace; staying put and just dropping the
    // invitation reads as nothing happened (MUL-820).
    onSuccess: (workspaces, invitation) => {
      const joined = workspaces.find((w) => w.id === invitation.workspace_id);
      if (joined) push(paths.workspace(joined.slug).issues());
    },
    // On failure the invitation was usually concluded on another surface
    // while it was on screen, or the join went through and a later step
    // failed; refetch both so the page reflects what the server has.
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
      queryClient.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });

  const decline = useMutation({
    mutationFn: (invitation: Invitation) => api.declineInvitation(invitation.id),
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
    },
  });

  return { accept, decline };
}
