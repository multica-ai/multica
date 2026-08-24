"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { ActivatePage } from "@multica/views/auth";

// Device authorization approval page (`multica login --device`). Requires a
// signed-in session: the whole point is approving a remote CLI from a device
// that already has credentials.
export default function ActivateDevicePage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(
        `${paths.login()}?next=${encodeURIComponent("/activate")}`,
      );
    }
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <ActivatePage />
    </div>
  );
}
