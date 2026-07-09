/**
 * Stub route. The "More" tab in (tabs)/_layout.tsx intercepts tabPress and
 * pushes /[workspace]/menu (formSheet route) instead of navigating here,
 * so this screen is never rendered through normal use. expo-router still
 * requires a file to exist at this path to register the Tabs.Screen entry.
 *
 * If a deep link or stale tab state somehow lands the user here, bounce
 * to inbox so they don't see a blank screen.
 */
import { Redirect } from "expo-router";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function MoreStub() {
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  return <Redirect href={slug ? `/${slug}/inbox` : "/select-workspace"} />;
}
