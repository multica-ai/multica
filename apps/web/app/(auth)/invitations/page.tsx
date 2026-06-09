"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { InvitationsPage } from "@multica/views/invitations";

export default function InvitationsRoutePage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  // Unauthenticated users have nowhere meaningful to land here — kick them
  // through login and bring them back. The login page will eventually run
  // its own listMyInvitations() check and route them here again.
  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(
        `${paths.login()}?next=${encodeURIComponent(paths.invitations())}`,
      );
    }
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return <InvitationsPage />;
}
