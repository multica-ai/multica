import { useEffect, useRef } from "react";
import { Slot, useRouter, useSegments } from "expo-router";
import { useQueryClient } from "@tanstack/react-query";
import { StatusBar } from "expo-status-bar";
import { ActivityIndicator, View, StyleSheet } from "react-native";
import {
  AppProviders,
  WorkspaceProvider,
  useAuthStore,
  useWorkspaceStore,
} from "@/lib/providers";
import { ensureWorkspaceForUser } from "@/lib/workspace-bootstrap";

function AuthGuard() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);
  const segments = useSegments();
  const router = useRouter();
  const qc = useQueryClient();
  const ensuringWorkspaceFor = useRef<string | null>(null);
  const inAuthGroup = segments[0] === "(auth)";

  useEffect(() => {
    if (isLoading) return;

    if (!user && !inAuthGroup) {
      router.replace("/(auth)/login");
    } else if (user && inAuthGroup && workspace) {
      router.replace("/(app)/(home)");
    }
  }, [user, isLoading, inAuthGroup, workspace, router]);

  useEffect(() => {
    if (isLoading || !user || workspace) return;
    if (ensuringWorkspaceFor.current === user.id) return;

    ensuringWorkspaceFor.current = user.id;
    ensureWorkspaceForUser(user.email, qc).catch(() => {
      ensuringWorkspaceFor.current = null;
    });
  }, [user, isLoading, workspace, qc]);

  if (isLoading || (user && !inAuthGroup && !workspace)) {
    return (
      <View style={styles.loading}>
        <ActivityIndicator size="large" color="#18181B" />
      </View>
    );
  }

  if (user && !inAuthGroup) {
    return (
      <WorkspaceProvider>
        <Slot />
      </WorkspaceProvider>
    );
  }

  return <Slot />;
}

export default function RootLayout() {
  return (
    <AppProviders>
      <StatusBar style="auto" />
      <AuthGuard />
    </AppProviders>
  );
}

const styles = StyleSheet.create({
  loading: {
    flex: 1,
    justifyContent: "center",
    alignItems: "center",
    backgroundColor: "#FFFFFF",
  },
});
