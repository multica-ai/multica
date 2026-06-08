"use client";

// CEREBRO-PATCH(terminal-popout-route): standalone full-window terminal
// loaded via window.open() from PresentationModeToggle. Lives outside the
// (dashboard) group so the sidebar/chrome is not rendered — pop-out view
// is meant to feel like a native terminal window.
//
// Workspace slug is bound into the platform singleton from the URL during
// render (rather than relying solely on [workspaceSlug]/layout.tsx side-
// effects) so the very first poll for `getActiveTerminalSession` carries
// the `X-Workspace-Slug` header. setCurrentWorkspace is idempotent on
// slug equality, so this is a no-op when the parent layout has already
// set it. The UUID mirror is filled in once the workspace list resolves.

import { use } from "react";
import { useQuery } from "@tanstack/react-query";
import { setCurrentWorkspace } from "@multica/core/platform";
import { workspaceBySlugOptions } from "@multica/core/workspace";
import { useAuthStore } from "@multica/core/auth";
import { TerminalPanel } from "@multica/cerebro-terminal";

export default function TerminalPopoutRoute({
  params,
}: {
  params: Promise<{ workspaceSlug: string; runtimeId: string }>;
}) {
  const { workspaceSlug, runtimeId } = use(params);
  const user = useAuthStore((s) => s.user);
  const { data: workspace } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug),
    enabled: !!user,
  });

  // Render-phase sync (matches [workspaceSlug]/layout.tsx) — guarantees
  // getCurrentSlug() returns the slug before any child useEffect (e.g.
  // useTerminalSession's first poll) reads it.
  setCurrentWorkspace(workspaceSlug, workspace?.id ?? null);

  return (
    <div
      style={{
        position: "fixed",
        inset: 0,
        background: "#0b0d0e",
        padding: 8,
        boxSizing: "border-box",
      }}
    >
      <TerminalPanel runtimeId={runtimeId} height={undefined} />
    </div>
  );
}
