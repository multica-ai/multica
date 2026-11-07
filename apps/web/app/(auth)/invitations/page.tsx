"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { InvitationsPage } from "@multica/views/invitations";
import { returnToCurrentPage } from "@/features/auth/return-to";

export default function InvitationsRoutePage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(returnToCurrentPage());
    }
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return <InvitationsPage />;
}
