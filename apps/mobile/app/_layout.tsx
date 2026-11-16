import "../global.css";

import { useEffect, useRef } from "react";
import { ActivityIndicator, AppState, View, type AppStateStatus } from "react-native";
import { Stack, ThemeProvider, router } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { KeyboardProvider } from "react-native-keyboard-controller";
import { QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import { PortalHost } from "@rn-primitives/portal";
import { ActionSheetProvider } from "@expo/react-native-action-sheet";
import { api } from "@/data/api";
import { maybeRenewSession } from "@/data/session-renewal";
import { queryClient } from "@/data/query-client";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { SessionActivityBoundary } from "@/components/auth/session-activity-boundary";
import { LightboxProvider, prewarmHighlighter } from "@/lib/markdown";
import { NAV_THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";
import { MobileI18nProvider } from "@/lib/i18n";
import { useServerConfigStore } from "@/data/server-config-store";
import { ServerSettings } from "@/components/auth/server-settings";

// Kick off Shiki highlighter init at module load — fires once per process,
// finishes before the user navigates to any screen with a code block. If
// init fails (engine unavailable) the highlighter falls back to plain
// text; nothing here is allowed to throw.
prewarmHighlighter();

function AuthInitializer({ children }: { children: React.ReactNode }) {
  const initialize = useAuthStore((s) => s.initialize);
  const qc = useQueryClient();
  // Idempotent guard: 401 on multiple in-flight requests would otherwise
  // logout/navigate repeatedly during the same session-expire moment.
  const signingOutRef = useRef(false);
  const initialized = useServerConfigStore((s) => s.initialized);
  const configError = useServerConfigStore((s) => s.error);
  const authLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    // Wire 401 handling onto the shared ApiClient singleton. Must be set
    // before any request fires — initialize() below kicks off the first
    // getMe() call, so do this synchronously first.
    api.setOptions({
      onUnauthorized: () => {
        if (signingOutRef.current) return;
        signingOutRef.current = true;
        void (async () => {
          await useAuthStore.getState().logout();
          await useWorkspaceStore.getState().clear();
          qc.clear();
          router.replace("/login");
          // Reset on next tick so a fresh session can hit 401 again later
          // without being silently swallowed.
          setTimeout(() => {
            signingOutRef.current = false;
          }, 0);
        })();
      },
    });
    // Launch check for the sliding session (MUL-7436). Runs after the token
    // is restored and the identity probe has settled, so it never races the
    // first getMe(); a no-op when there is no session to extend.
    void initialize().then(() => maybeRenewSession()).catch(() => {
      useAuthStore.setState({ isLoading: false });
    });
  }, [initialize, qc]);

  // Foreground transitions are one of the two "someone is using this" signals;
  // SessionActivityBoundary below supplies the other, so an app that stays
  // foregrounded for longer than the check interval still renews. Deliberately
  // not a timer: a backgrounded or untouched app must not keep the session of
  // someone who stopped using it alive.
  useEffect(() => {
    const sub = AppState.addEventListener("change", (status: AppStateStatus) => {
      if (status === "active" && !useAuthStore.getState().isLoading &&
          useServerConfigStore.getState().initialized &&
          !useServerConfigStore.getState().switching) maybeRenewSession();
    });
    return () => sub.remove();
  }, []);

  if (configError) return <ServerSettings recovery />;
  if (!initialized || authLoading) {
    return <View className="flex-1 items-center justify-center bg-background"><ActivityIndicator /></View>;
  }
  return <>{children}</>;
}

export default function RootLayout() {
  const { colorScheme, isDarkColorScheme } = useColorScheme();
  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <MobileI18nProvider>
        <SafeAreaProvider>
          <KeyboardProvider>
            <QueryClientProvider client={queryClient}>
              <ThemeProvider value={NAV_THEME[colorScheme]}>
                <AuthInitializer>
                  <ActionSheetProvider>
                    <SessionActivityBoundary>
                      <LightboxProvider>
                        <StatusBar style={isDarkColorScheme ? "light" : "dark"} />
                        <Stack screenOptions={{ headerShown: false }}>
                          <Stack.Screen name="index" />
                          <Stack.Screen name="(auth)" />
                          <Stack.Screen name="(app)" />
                        </Stack>
                        <PortalHost />
                      </LightboxProvider>
                    </SessionActivityBoundary>
                  </ActionSheetProvider>
                </AuthInitializer>
              </ThemeProvider>
            </QueryClientProvider>
          </KeyboardProvider>
        </SafeAreaProvider>
      </MobileI18nProvider>
    </GestureHandlerRootView>
  );
}
