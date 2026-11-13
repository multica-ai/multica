import { useState } from "react";
import { StatusBar } from "expo-status-bar";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { useAuthStore } from "@multica/core/auth";
import type { DeepLink } from "../platform/deep-link";
import { endpoints } from "../platform/env";
import { MobileCoreProvider } from "../platform/mobile-core-provider";
import { useSignOut } from "../platform/session";
import { useDeepLinks } from "../platform/use-deep-links";
import { selectWorkspace, useWorkspaceSelection } from "../platform/workspace";

// Placeholder until the interface direction is chosen: shows that the core
// session, workspace resolution, deep links and server wiring come up on device.
function SessionStatus() {
  const auth = useAuthStore((state) => state.status);
  const user = useAuthStore((state) => state.user);
  const selection = useWorkspaceSelection();
  const signOut = useSignOut();
  const [lastLink, setLastLink] = useState<DeepLink | null>(null);

  useDeepLinks((link) => {
    if (link.kind === "open") selectWorkspace(link.workspaceSlug);
    setLastLink(link);
  });

  const workspace = selection.status === "ready" ? selection.workspace.name : selection.status;
  return (
    <View style={styles.screen}>
      <Text style={styles.title}>Multica</Text>
      <Text style={styles.line}>Server: {endpoints.apiUrl}</Text>
      <Text style={styles.line}>Session: {user ? `${auth} as ${user.email}` : auth}</Text>
      <Text style={styles.line}>Workspace: {workspace}</Text>
      {lastLink && <Text style={styles.line}>Last link: {JSON.stringify(lastLink)}</Text>}
      {user && (
        <Pressable accessibilityRole="button" onPress={signOut}>
          <Text style={styles.action}>Sign out</Text>
        </Pressable>
      )}
      <StatusBar style="auto" />
    </View>
  );
}

export function App() {
  return (
    <MobileCoreProvider>
      <SessionStatus />
    </MobileCoreProvider>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, justifyContent: "center", padding: 24, gap: 8 },
  title: { fontSize: 28, fontWeight: "600" },
  line: { fontSize: 15 },
  action: { fontSize: 15, color: "#2563eb", paddingVertical: 8 },
});
