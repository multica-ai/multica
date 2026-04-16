"use client";

import type { Workspace } from "@multica/core/types";
import { CreateWorkspaceForm } from "./create-workspace-form";

/**
 * Full-page shell for the /new-workspace route. Shared between web
 * (Next.js) and desktop (react-router) so the two apps can't drift.
 * Callers provide the onSuccess handler — that's the only app-specific
 * piece, because each app uses its own navigation primitive.
 */
export function NewWorkspacePage({
  onSuccess,
}: {
  onSuccess: (workspace: Workspace) => void;
}) {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center bg-background px-6 py-12">
      <div className="flex w-full max-w-md flex-col items-center gap-6">
        <div className="text-center">
          <h1 className="text-3xl font-semibold tracking-tight">
            Welcome to Multica
          </h1>
          <p className="mt-2 text-muted-foreground">
            Create your workspace to get started.
          </p>
        </div>
        <CreateWorkspaceForm onSuccess={onSuccess} />
      </div>
    </div>
  );
}
