"use client";

import { ArrowLeft, LogOut } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import type { Workspace } from "@multica/core/types";
import { useLogout } from "../auth";
import { LocaleSwitcher, useI18n } from "../i18n";
import { DragStrip } from "../platform";
import { CreateWorkspaceForm } from "./create-workspace-form";

/**
 * Full-page shell for the "create workspace" transition. Shared between web
 * (Next.js route `/workspaces/new`) and desktop (window-overlay). The
 * top-bar affordances — Back (when dismissable) and Log out — live here
 * so both platforms get identical UX; platform-specific concerns like
 * window-drag region and macOS traffic-light handling stay in each app's
 * shell.
 *
 * `onBack` is optional: caller passes it only when there's somewhere to go
 * back to (user has other workspaces, or the flow was entered from an
 * existing session). On the zero-workspace entry path it's omitted, which
 * hides Back — Log out is then the only escape.
 */
export function NewWorkspacePage({
  onSuccess,
  onBack,
}: {
  onSuccess: (workspace: Workspace) => void;
  onBack?: () => void;
}) {
  const { t } = useI18n();
  const logout = useLogout();

  return (
    <div className="relative flex min-h-svh flex-col bg-background">
      <DragStrip />
      {onBack && (
        <Button
          variant="ghost"
          size="sm"
          className="absolute top-16 left-12 text-muted-foreground"
          onClick={onBack}
        >
          <ArrowLeft />
          {t("workspace.back")}
        </Button>
      )}
      <div className="absolute top-12 right-12 flex items-center gap-2">
        <LocaleSwitcher />
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground hover:text-destructive"
          onClick={logout}
        >
          <LogOut />
          {t("workspace.logOut")}
        </Button>
      </div>

      <div className="flex flex-1 flex-col items-center justify-center px-6 pb-12">
        <div className="flex w-full max-w-md flex-col items-center gap-6">
          <div className="text-center">
            <h1 className="text-3xl font-semibold tracking-tight">
              {t("workspace.new.title")}
            </h1>
            <p className="mt-2 text-muted-foreground">
              {t("workspace.new.description")}
            </p>
          </div>
          <CreateWorkspaceForm onSuccess={onSuccess} />
          <p className="text-center text-xs text-muted-foreground">
            You can invite teammates once your workspace is ready.
          </p>
        </div>
      </div>
    </div>
  );
}
