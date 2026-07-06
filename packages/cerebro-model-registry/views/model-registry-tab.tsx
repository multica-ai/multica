"use client";

// CEREBRO (FIR-2698): Settings tab for the single-source model registry —
// prices, context windows, and display labels for every model, editable
// through the same propose → review → approve pattern already used for
// Agent Context (packages/cerebro-agent-context). Replaces four hand-
// maintained in-code price tables (server/pkg/pricing, server/internal/
// metrics, server/internal/cerebro/sessions/context_window.go, and the
// frontend's packages/views/runtimes/utils.ts MODEL_PRICING) that drifted
// apart — the FIR-2689 root cause.

import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { modelRegistryOptions } from "../core/queries";
import { ModelRegistryProposeDialog } from "./components/model-registry-propose-dialog";
import { ModelRegistryTable } from "./components/model-registry-table";
import { ModelRegistryChangeRequestQueue } from "./components/model-registry-change-request-queue";
import { ModelRegistryVersionsPanel } from "./components/model-registry-versions-panel";

export function ModelRegistryTab() {
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: registry, isLoading } = useQuery(modelRegistryOptions());

  const currentMember = members.find((m) => m.user_id === userId) ?? null;
  // Mirrors the backend's canManage check exactly (modelregistry/handler.go):
  // workspace owner/admin, OR the registry's own owner, OR a named approver.
  // The backend enforces this regardless of what this shows, but the button
  // must be visible to everyone the backend actually lets through — an
  // approver who isn't a workspace admin still needs to see Approve/Reject.
  const canManage =
    currentMember?.role === "owner" ||
    currentMember?.role === "admin" ||
    (!!userId && registry?.owner_id === userId) ||
    (!!userId && (registry?.approver_ids ?? []).includes(userId));

  return (
    <div className="space-y-5 p-4 md:p-6">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold">Model registry</h3>
          <p className="mt-0.5 max-w-prose text-xs text-muted-foreground">
            The single source for every model&apos;s price, context window, and
            display label — read by both the backend cost calculator and every
            cost chart in the app. Edits are versioned: propose a change,
            review the diff, approve it, and it goes live immediately.
          </p>
        </div>
        {registry && (
          <ModelRegistryProposeDialog
            currentVersion={registry.current_version}
            canReview={canManage}
          />
        )}
      </div>

      <div className="my-1 h-px bg-border" />

      {isLoading && <div className="text-xs text-muted-foreground">Loading…</div>}
      {registry && (
        <div className="space-y-5">
          <ModelRegistryTable
            snapshot={registry.snapshot}
            currentVersion={registry.current_version}
            canReview={canManage}
          />
          <ModelRegistryChangeRequestQueue members={members} canReview={canManage} />
          <ModelRegistryVersionsPanel members={members} canManage={canManage} />
        </div>
      )}
    </div>
  );
}
