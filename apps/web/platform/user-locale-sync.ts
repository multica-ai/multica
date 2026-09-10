"use client";

import { usePathname } from "next/navigation";
import { paths } from "@multica/core/paths";

export function useUserLocaleSyncEnabled(): boolean {
  const pathname = usePathname();
  // Account locale synchronization can reload the page. Finish the single-use
  // OAuth callback before allowing that reload on its destination.
  return !!pathname && pathname.replace(/\/+$/, "") !== paths.authCallback();
}
