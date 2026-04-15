"use client";

import { useEffect } from "react";
import { useRouter, useParams } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { InvitePage } from "@multica/views/invite";

export default function InviteAcceptPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  // Redirect to login if not authenticated, with a redirect back to this page.
  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(`/login?next=/invite/${params.id}`);
    }
  }, [isLoading, user, router, params.id]);

  if (isLoading || !user) return null;

  return <InvitePage invitationId={params.id} />;
}
