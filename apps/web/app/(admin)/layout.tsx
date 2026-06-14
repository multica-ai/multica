"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";

export default function AdminLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && (!user || !user.is_super_admin)) {
      router.replace(paths.login());
    }
  }, [user, isLoading, router]);

  if (isLoading || !user?.is_super_admin) {
    return null;
  }

  return (
    <div className="min-h-screen bg-background">
      {children}
    </div>
  );
}
