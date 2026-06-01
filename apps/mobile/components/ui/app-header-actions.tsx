/**
 * Header utility buttons shared across primary tabs (Inbox / My Issues).
 * Provides two global actions on the right: search and create-issue.
 *
 * The workspace menu (global nav, workspace switcher, settings) is reached
 * via the "More" tab in the bottom bar.
 *
 * Tab-specific actions (e.g. My Issues filter) MUST NOT live here — they
 * mix scope levels with global actions and would clutter the strip.
 */
import { router } from "expo-router";
import { IconButton } from "@/components/ui/icon-button";
import { useWorkspaceStore } from "@/data/workspace-store";

export function HeaderActions() {
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

  const onSearch = () => {
    if (slug) router.push(`/${slug}/search`);
  };
  const onCreate = () => {
    if (slug) router.push(`/${slug}/new-issue`);
  };

  return (
    <>
      <IconButton
        name="search"
        onPress={onSearch}
        accessibilityLabel="Search"
      />
      <IconButton
        name="add"
        iconSize={24}
        onPress={onCreate}
        accessibilityLabel="New issue"
      />
    </>
  );
}
