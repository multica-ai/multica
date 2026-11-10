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
 * Accept / decline for a pending invitation, shared by the invitations page
 * and the workspace switcher menu so both surfaces join the same way.
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
      // un-onboarded state before guards look at it.
      if (!hasOnboarded) {
        await api.markOnboardingComplete({
          completion_path: "invite_accept",
          workspace_id: invitation.workspace_id,
        });
        await useAuthStore.getState().refreshMe();
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
    // while it was on screen; refetch so the stale row drops.
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: workspaceKeys.myInvitations() });
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
