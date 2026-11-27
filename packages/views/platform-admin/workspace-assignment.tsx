"use client";

import { useEffect, useRef, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { paths } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { useLogout } from "../auth";
import { useT } from "../i18n";
import { AppLink, useNavigation } from "../navigation";
import { DragStrip } from "../platform";

export function useCanCreateWorkspace() {
  const accountPermission = useAuthStore((s) => s.user?.can_create_workspace);
  const creationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);
  return accountPermission ?? !creationDisabled;
}

export function WorkspaceAssignmentGate({
  workspaceCount,
  ready,
  onAssigned,
  onRetry,
  unavailable = false,
  children,
}: {
  workspaceCount: number;
  ready: boolean;
  onAssigned: (path: string) => void;
  onRetry: () => void;
  unavailable?: boolean;
  children: ReactNode;
}) {
  const canCreate = useCanCreateWorkspace();
  const isAdmin = useAuthStore((s) => s.user?.platform_admin === true);
  const { t } = useT("workspace");
  if (!canCreate && !ready) {
    return (
      <div className="flex h-full flex-col">
        <DragStrip />
        <div className="m-auto flex flex-col items-center gap-3 px-6 text-body text-muted-foreground">
          {unavailable ? (
            <>
              <p role="alert">{t(($) => $.platform_admin.load_error)}</p>
              <Button variant="outline" onClick={onRetry}>{t(($) => $.platform_admin.retry)}</Button>
            </>
          ) : (
            <p role="status">{t(($) => $.platform_admin.loading)}</p>
          )}
        </div>
      </div>
    );
  }
  if (!canCreate && workspaceCount === 0) {
    return <WaitingForWorkspace onAssigned={onAssigned} />;
  }
  if (!isAdmin || !ready || workspaceCount > 0) return children;
  return (
    <>
      <AppLink href={paths.admin()} className="fixed bottom-4 right-4 z-10 rounded-md border border-border bg-background px-3 py-2 text-body hover:bg-surface-selected">
        {t(($) => $.platform_admin.title)}
      </AppLink>
      {children}
    </>
  );
}

function WaitingForWorkspace({ onAssigned }: { onAssigned: (path: string) => void }) {
  const { t } = useT("workspace");
  const user = useAuthStore((s) => s.user);
  const queryClient = useQueryClient();
  const logout = useLogout();
  const { push } = useNavigation();
  const assignedRef = useRef(false);
  const status = useQuery({
    queryKey: ["workspace-assignment", user?.id],
    queryFn: async ({ signal }) => {
      const [account, workspaces] = await Promise.all([api.getMe(), api.listWorkspaces(signal)]);
      if (account.id !== user?.id) throw new Error("Workspace assignment could not be verified");
      return { account, workspaces };
    },
    staleTime: 0,
    refetchInterval: 5000,
  });

  useEffect(() => {
    const current = status.data;
    if (!current || current.account.id !== user?.id || assignedRef.current) return;
    const first = current.workspaces[0];
    if (first && (current.account.onboarded_at || user.onboarded_at)) {
      assignedRef.current = true;
      useAuthStore.getState().setUser(current.account);
      queryClient.setQueryData(workspaceKeys.list(), current.workspaces);
      onAssigned(paths.workspace(first.slug).issues());
    } else if (first || current.account.can_create_workspace) {
      useAuthStore.getState().setUser(current.account);
      queryClient.setQueryData(workspaceKeys.list(), current.workspaces);
    }
  }, [status.data, user?.id, user?.onboarded_at, queryClient, onAssigned]);

  return (
    <div className="flex h-full flex-col bg-background">
      <DragStrip />
      <main className="m-auto flex w-full max-w-md flex-col items-center gap-4 px-6 py-12 text-center">
        <h1 className="text-display-sm font-semibold">{t(($) => $.platform_admin.waiting)}</h1>
        <p className="text-body text-muted-foreground">{t(($) => $.platform_admin.waiting_description)}</p>
        <p className="max-w-full break-all text-caption text-muted-foreground">{user?.email}</p>
        {status.isError && (
          <p role="alert" className="text-body text-destructive">{t(($) => $.platform_admin.load_error)}</p>
        )}
        <div className="flex flex-wrap justify-center gap-2">
          <Button variant="outline" onClick={() => void status.refetch()} disabled={status.isFetching}>
            {t(($) => $.platform_admin.check_assignment)}
          </Button>
          {user?.platform_admin && (
            <Button variant="outline" onClick={() => push(paths.admin())}>
              {t(($) => $.platform_admin.title)}
            </Button>
          )}
          <Button variant="ghost" onClick={logout}>{t(($) => $.platform_admin.sign_out)}</Button>
        </div>
      </main>
    </div>
  );
}
