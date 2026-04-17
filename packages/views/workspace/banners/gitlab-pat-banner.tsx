"use client";

import { useState } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useWorkspaceGitlabConnection } from "@multica/core/gitlab/queries";
import { useUserGitlabConnection } from "@multica/core/gitlab/user-queries";
import { useNavigation } from "../../navigation";
import { Button } from "@multica/ui/components/ui/button";
import { X } from "lucide-react";

const STORAGE_KEY_PREFIX = "multica.gitlab-pat-banner-dismissed:";

export function GitlabPatBanner() {
  const workspace = useCurrentWorkspace();
  const workspaceId = workspace?.id ?? "";
  const workspaceSlug = workspace?.slug ?? "";

  const { data: wsConn } = useWorkspaceGitlabConnection(workspaceId);
  const { data: userConn } = useUserGitlabConnection(workspaceId);
  const { push } = useNavigation();

  const [dismissed, setDismissed] = useState(() => {
    if (!workspaceId) return false;
    try {
      return localStorage.getItem(STORAGE_KEY_PREFIX + workspaceId) === "1";
    } catch {
      return false;
    }
  });

  if (!workspace) return null;
  if (dismissed) return null;
  if (!wsConn?.gitlab_project_id) return null;
  if (userConn?.connected) return null;

  return (
    <div className="bg-muted/50 border-b border-border px-6 py-3 flex items-center justify-between gap-4">
      <p className="text-sm">
        Your writes are posting to GitLab as the workspace service account.{" "}
        <a
          className="underline cursor-pointer"
          onClick={() => push(`/${workspaceSlug}/settings`)}
        >
          Connect your GitLab account
        </a>{" "}
        so they&apos;re attributed to you.
      </p>
      <Button
        size="sm"
        variant="ghost"
        onClick={() => {
          setDismissed(true);
          try {
            localStorage.setItem(STORAGE_KEY_PREFIX + workspaceId, "1");
          } catch {
            /* ignore quota / private mode */
          }
        }}
        aria-label="Dismiss"
      >
        <X className="h-4 w-4" />
      </Button>
    </div>
  );
}
