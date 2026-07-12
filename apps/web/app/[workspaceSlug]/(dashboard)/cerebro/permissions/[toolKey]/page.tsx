"use client";

// FIR-3091 punkt 8 (fase 1b) — per-permission detail route. Mirrors groups/[id]:
// the shared view lives in @multica/cerebro-tool-policy/views; this file is just
// the Next.js wiring. Gating on cerebro_permission_detail happens inside the
// view, so the route stays registered but dormant while the flag is off.

import { use } from "react";
import { PermissionDetailPage } from "@multica/cerebro-tool-policy/views";
import { useNavigation } from "@multica/views/navigation";

export default function PermissionDetailRoute({
  params,
}: {
  params: Promise<{ workspaceSlug: string; toolKey: string }>;
}) {
  const { workspaceSlug, toolKey } = use(params);
  const navigation = useNavigation();
  return (
    <PermissionDetailPage
      toolKey={decodeURIComponent(toolKey)}
      onBack={() =>
        navigation.push(`/${workspaceSlug}/settings?tab=permissions`)
      }
    />
  );
}
