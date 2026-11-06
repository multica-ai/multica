"use client";

import { useAuthStore } from "@multica/core/auth";
import { InvitationsPage } from "@multica/views/invitations";
import { useLoginRedirect } from "@/features/auth/use-login-redirect";

export default function InvitationsRoutePage() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useLoginRedirect();

  if (isLoading || !user) return null;

  return <InvitationsPage />;
}
