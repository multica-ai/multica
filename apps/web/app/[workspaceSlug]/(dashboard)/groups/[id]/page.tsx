"use client";

// JEH-1067 (Bundle B) — Cerebro Groups detail route. Lives under the
// workspace-scoped /(dashboard) tree mirroring runtimes/[id]. The actual
// view component lives in @multica/cerebro-groups/views; this file is just
// the Next.js wiring: pulls the id from the URL and supplies an onBack
// callback that returns to Settings → Groups.

import { use } from "react";
import { GroupDetailView } from "@multica/cerebro-groups/views";
import { useNavigation } from "@multica/views/navigation";

export default function GroupDetailRoute({
  params,
}: {
  params: Promise<{ workspaceSlug: string; id: string }>;
}) {
  const { workspaceSlug, id } = use(params);
  const navigation = useNavigation();
  return (
    <GroupDetailView
      groupId={id}
      onBack={() => navigation.push(`/${workspaceSlug}/settings?tab=groups`)}
    />
  );
}
