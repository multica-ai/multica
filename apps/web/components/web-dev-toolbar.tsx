"use client";

import { usePathname } from "next/navigation";
import { DevToolbar } from "@multica/views/dev-toolbar";
import { useNavigation } from "@multica/views/navigation";

export function WebDevToolbar({ apiUrl }: { apiUrl: string }) {
  const pathname = usePathname();
  const navigation = useNavigation();
  return (
    <DevToolbar build="web" apiUrl={apiUrl} route={pathname} onNavigate={(path) => navigation.push(path)} />
  );
}
