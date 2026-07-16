import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  createMemoryRouter,
  RouterProvider,
  useParams,
  useRouteError,
} from "react-router-dom";
import { AlertTriangle, RotateCw, X } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { setCurrentWorkspace } from "@multica/core/platform";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { workspaceBySlugOptions } from "@multica/core/workspace";
import { Button } from "@multica/ui/components/ui/button";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { ModalRegistry } from "@multica/views/modals/registry";
import { WorkspacePresencePrefetch } from "@multica/views/layout";
import { DragStrip } from "@multica/views/platform";
import type { IssueWindowContext } from "../../../shared/issue-window";
import { IssueDetailPage } from "../pages/issue-detail-page";
import { IssueWindowNavigationProvider } from "../platform/issue-window-navigation";

export function IssueWindow({ context }: { context: IssueWindowContext }) {
  const router = useMemo(
    () =>
      createMemoryRouter(
        [
          {
            path: ":workspaceSlug/issues/:id",
            element: <IssueWindowRoute />,
            errorElement: <IssueWindowRouteError />,
          },
        ],
        { initialEntries: [context.path] },
      ),
    [context.path],
  );

  return <RouterProvider router={router} />;
}

function IssueWindowRoute() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>();
  const user = useAuthStore((state) => state.user);
  const { data: workspace, isFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug ?? ""),
    enabled: !!user && !!workspaceSlug,
  });

  if (workspace && workspaceSlug) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  }

  if (!isFetched) {
    return (
      <IssueWindowFrame>
        <div className="flex min-h-0 flex-1 items-center justify-center">
          <MulticaIcon className="size-6 animate-pulse" />
        </div>
      </IssueWindowFrame>
    );
  }

  if (!workspace || !workspaceSlug) {
    return <IssueWindowUnavailable />;
  }

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      <IssueWindowNavigationProvider>
        <WorkspacePresencePrefetch />
        <IssueWindowFrame>
          <IssueDetailPage onDelete={() => window.desktopAPI.closeWindow()} />
        </IssueWindowFrame>
        <ModalRegistry />
      </IssueWindowNavigationProvider>
    </WorkspaceSlugProvider>
  );
}

function IssueWindowFrame({ children }: { children: React.ReactNode }) {
  return (
    <div
      data-dedicated-issue-window="true"
      className="flex h-screen min-h-0 flex-col bg-page-canvas text-foreground"
    >
      <DragStrip />
      <div className="flex min-h-0 flex-1 overflow-hidden">{children}</div>
    </div>
  );
}

function IssueWindowUnavailable() {
  return (
    <IssueWindowFrame>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-4 p-8 text-center">
        <div className="rounded-full bg-muted p-3 text-muted-foreground">
          <AlertTriangle className="size-6" aria-hidden="true" />
        </div>
        <div className="space-y-1">
          <h1 className="text-lg font-semibold">Issue unavailable</h1>
          <p className="text-sm text-muted-foreground">
            This workspace is no longer available in your account.
          </p>
        </div>
        <Button variant="outline" onClick={() => window.desktopAPI.closeWindow()}>
          Close window
        </Button>
      </div>
    </IssueWindowFrame>
  );
}

function IssueWindowRouteError() {
  const error = useRouteError();
  const message = error instanceof Error ? error.message : "Unknown route error";

  return (
    <IssueWindowFrame>
      <div
        role="alert"
        className="flex min-h-0 flex-1 flex-col items-center justify-center gap-4 p-8 text-center"
      >
        <div className="rounded-full bg-destructive/10 p-3 text-destructive">
          <AlertTriangle className="size-6" aria-hidden="true" />
        </div>
        <div className="space-y-1">
          <h1 className="text-lg font-semibold">Something went wrong</h1>
          <p className="max-w-lg truncate text-sm text-muted-foreground">
            {message}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => window.location.reload()}>
            <RotateCw className="size-4" aria-hidden="true" />
            Reload
          </Button>
          <Button variant="outline" onClick={() => window.desktopAPI.closeWindow()}>
            <X className="size-4" aria-hidden="true" />
            Close window
          </Button>
        </div>
      </div>
    </IssueWindowFrame>
  );
}
