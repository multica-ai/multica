"use client";

import { useWorkspaceId } from "@multica/core";
import { useWorkspacePresencePrefetch } from "@multica/core/agents";
import { useIsGuest } from "@multica/core/permissions";

// Mount once inside any subtree that's already gated on "workspace resolved"
// (DashboardLayout on web, WorkspaceRouteLayout on desktop). useWorkspaceId
// throws when called outside a resolved workspace — the gating in those
// layouts guarantees this component never sees that state.
export function WorkspacePresencePrefetch() {
  const wsId = useWorkspaceId();
  // Agent presence is not shown to guests, and the server refuses it.
  return useIsGuest(wsId) ? null : <PresencePrefetch wsId={wsId} />;
}

function PresencePrefetch({ wsId }: { wsId: string }) {
  useWorkspacePresencePrefetch(wsId);
  return null;
}
