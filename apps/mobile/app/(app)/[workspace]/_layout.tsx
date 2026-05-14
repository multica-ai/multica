import { useEffect } from "react";
import { Redirect, Stack, useLocalSearchParams } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { workspaceListOptions } from "@/data/queries/workspaces";
import { useWorkspaceStore } from "@/data/workspace-store";
import { RealtimeProvider } from "@/data/realtime/realtime-provider";
import { useInboxRealtime } from "@/data/realtime/use-inbox-realtime";
import { useMyIssuesRealtime } from "@/data/realtime/use-my-issues-realtime";
import { useChatSessionsRealtime } from "@/data/realtime/use-chat-sessions-realtime";
import { ModalCloseButton } from "@/components/ui/modal-close-button";

/**
 * Mounts every per-feature realtime subscription. Lives inside
 * RealtimeProvider so the WSClient context is available, and stays alive
 * for the whole workspace session — the inbox unread count must keep
 * refreshing even while the user is on an issue page or settings, not
 * just when the inbox tab is foregrounded.
 *
 * Add new realtime feature hooks here as they land (issue, chat, etc).
 */
function RealtimeSubscriptions() {
  useInboxRealtime();
  useMyIssuesRealtime();
  useChatSessionsRealtime();
  return null;
}

/**
 * Workspace context layout. Reads the slug from the URL (the route is the
 * source of truth — see apps/mobile/CLAUDE.md "Behavioral parity"), validates
 * membership against the workspaces list, then syncs id+slug into the
 * Zustand store so ApiClient.fetch can read the slug synchronously when
 * injecting the X-Workspace-Slug header.
 *
 * If the slug doesn't match any workspace the user belongs to, redirect to
 * /select-workspace (covers stale persisted slugs after the user lost
 * membership, deep links to wrong slugs, etc.).
 */
export default function WorkspaceLayout() {
  const { workspace: slug } = useLocalSearchParams<{ workspace: string }>();
  const { data: workspaces, isLoading } = useQuery(workspaceListOptions());
  const setCurrentWorkspace = useWorkspaceStore((s) => s.setCurrentWorkspace);

  const matched = workspaces?.find((w) => w.slug === slug);

  useEffect(() => {
    if (matched) {
      setCurrentWorkspace(matched.id, matched.slug);
    }
  }, [matched, setCurrentWorkspace]);

  // Wait for the workspaces list before deciding membership — otherwise a
  // valid deep link would briefly redirect away on cold start.
  if (isLoading) return null;

  if (!matched) return <Redirect href="/select-workspace" />;

  // Tabs hide their own header; pushed screens (issue/[id]) get a native
  // iOS Stack header with the standard back button + swipe-to-dismiss.
  return (
    <RealtimeProvider>
      <RealtimeSubscriptions />
      <Stack>
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen
          name="issue/[id]"
          options={{
            title: "Issue",
            headerBackTitle: "Back",
          }}
        />
        <Stack.Screen
          name="more/projects"
          options={{ title: "Projects", headerBackTitle: "More" }}
        />
        <Stack.Screen
          name="more/agents"
          options={{ title: "Agents", headerBackTitle: "More" }}
        />
        <Stack.Screen
          name="more/pins"
          options={{ title: "Pins", headerBackTitle: "More" }}
        />
        <Stack.Screen
          name="more/notifications"
          options={{ title: "Notifications", headerBackTitle: "More" }}
        />
        <Stack.Screen
          name="new-issue"
          options={{
            title: "New Issue",
            presentation: "modal",
            headerLeft: () => <ModalCloseButton />,
          }}
        />
        <Stack.Screen
          name="search"
          options={{
            title: "Search",
            presentation: "modal",
            headerLeft: () => <ModalCloseButton />,
          }}
        />
      </Stack>
    </RealtimeProvider>
  );
}
