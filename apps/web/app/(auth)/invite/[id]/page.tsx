"use client";

import { useRouter, useParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { InvitePage } from "@multica/views/invite";

export default function InviteAcceptPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const user = useAuthStore((s) => s.user);
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  const onBack =
    wsList.length > 0 ? () => router.push(paths.root()) : undefined;

  return <InvitePage invitationId={params.id} onBack={onBack} />;
}
