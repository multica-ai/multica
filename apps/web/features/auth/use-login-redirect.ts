"use client";

import { useEffect, useRef } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { returnToCurrentPage } from "./return-to";

export function useLoginRedirect(enabled = true) {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const expired = useAuthStore((s) => s.expired);
  const router = useRouter();
  const pathname = usePathname();
  const hadUser = useRef(!!user);

  useEffect(() => {
    if (user) {
      hadUser.current = true;
      return;
    }
    if (isLoading || !enabled) return;

    // An explicit logout clears expired. A page losing that session must not
    // carry the previous account's destination into the next sign-in.
    router.replace(
      hadUser.current && !expired ? paths.login() : returnToCurrentPage(),
    );
  }, [user, isLoading, expired, router, enabled, pathname]);
}
