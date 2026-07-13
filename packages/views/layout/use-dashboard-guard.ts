"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigationStore } from "@multica/core/navigation";
import { useAuthStore } from "@multica/core/auth";
import {
  paths,
  resolvePostAuthDestination,
  useCurrentWorkspace,
  useHasOnboarded,
} from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace";
import { useRecentIssuesStore } from "@multica/core/issues/stores";
import { useNavigation } from "../navigation";

/**
 * Auth + workspace gate for the dashboard.
 *
 * Redirect logic:
 *  - Auth still loading → wait
 *  - Not logged in → /login
 *  - Logged in but workspace list not yet loaded → wait (don't bounce prematurely)
 *  - Logged in but URL slug doesn't resolve to any workspace →
 *    `resolvePostAuthDestination(list, hasOnboarded)` (workspace-presence first;
 *    see paths/resolve.ts for the full table)
 *
 * The "un-onboarded but in workspace" state IS valid now — it's the
 * mid-flow window between "user picked a runtime on the onboarding screen
 * and got dropped into the workspace" and "user picked a starter prompt in
 * the workspace OnboardingHelperModal, which fires BootstrapOnboardingRuntime
 * and marks onboarded". This guard deliberately does NOT redirect that
 * state out: it only redirects when the URL slug doesn't resolve,
 * regardless of onboarded. The blocking modal inside the workspace shell
 * handles completion.
 *
 * (Older comment claimed this state was physically impossible because
 * CreateWorkspace and AcceptInvitation atomically marked onboarded.
 * CreateWorkspace no longer marks; AcceptInvitation still does — invitees
 * skip the modal entirely.)
 *
 * We read the workspace list query state directly (rather than relying on
 * useCurrentWorkspace's null return) so we can distinguish "list loading"
 * from "slug not found". Otherwise users could see a transient redirect
 * before their workspace list arrives.
 */
export function useDashboardGuard() {
  const { pathname, replace } = useNavigation();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useCurrentWorkspace();
  const hasOnboarded = useHasOnboarded();
  const { data: workspaces = [], isFetched: workspaceListFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      replace(paths.login());
      return;
    }
    if (!workspaceListFetched) return;
    if (!workspace) {
      replace(resolvePostAuthDestination(workspaces, hasOnboarded));
    }
  }, [user, isLoading, workspaceListFetched, workspace, workspaces, hasOnboarded, replace]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  // Drop recent-issues buckets for workspaces the user no longer belongs to.
  // Runs once the workspace list resolves, and again whenever membership
  // changes (workspace deleted, user kicked, user left).
  useEffect(() => {
    if (!workspaceListFetched) return;
    useRecentIssuesStore
      .getState()
      .pruneWorkspaces(workspaces.map((w) => w.id));
  }, [workspaceListFetched, workspaces]);

  return { user, isLoading, workspace };
}
