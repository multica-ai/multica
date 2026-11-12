"use client";

import { usePathname } from "next/navigation";
import { DevToolbar } from "@multica/views/dev-toolbar";

export function WebDevToolbar({ apiUrl }: { apiUrl: string }) {
  const pathname = usePathname();
  return <DevToolbar build="web" apiUrl={apiUrl} route={pathname} />;
}
