import "../global.css";

import { useEffect, useRef, useState } from "react";
import { AppState, View, type AppStateStatus } from "react-native";
import {
  Stack,
  ThemeProvider,
  router,
  type ErrorBoundaryProps,
} from "expo-router";
import { StatusBar } from "expo-status-bar";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import {
  SafeAreaInsetsContext,
  SafeAreaProvider,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import { KeyboardProvider } from "react-native-keyboard-controller";
import { QueryClientProvider, useQueryClient } from "@tanstack/react-query";
import { PortalHost } from "@rn-primitives/portal";
import { api } from "@/data/api";
import { maybeRenewSession } from "@/data/session-renewal";
import { queryClient } from "@/data/query-client";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { SessionActivityBoundary } from "@/components/auth/session-activity-boundary";
import { NetworkRefreshBridge } from "@/components/system/network-refresh-bridge";
import { OfflineBanner } from "@/components/system/offline-banner";
import {
  RootErrorBoundary,
  RouteErrorBoundary,
} from "@/components/system/root-error-boundary";
import { useConnectivity } from "@/components/system/use-connectivity";
import { isOffline } from "@/data/network-transitions";
import { LightboxProvider, prewarmHighlighter } from "@/lib/markdown";
import { NAV_THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";
import { MobileI18nProvider } from "@/lib/i18n";

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
    void initialize().then(() => maybeRenewSession());
  }, [initialize, qc]);

  // Foreground transitions are one of the two "someone is using this" signals;
  // SessionActivityBoundary below supplies the other, so an app that stays
  // foregrounded for longer than the check interval still renews. Deliberately
  // not a timer: a backgrounded or untouched app must not keep the session of
  // someone who stopped using it alive.
  useEffect(() => {
    const sub = AppState.addEventListener("change", (status: AppStateStatus) => {
      if (status === "active") maybeRenewSession();
    });
    return () => sub.remove();
  }, []);

  return <>{children}</>;
}

/**
 * Global chrome above the navigator. The offline strip is in normal flow, so
 * it never covers a header; while it is visible it consumes the top safe
 * area, and the context below is zeroed so JS headers don't pad for the notch
 * a second time. Native stack headers compute their inset from the shifted
 * view frame and need no adjustment.
 */
function AppChrome({ children }: { children: React.ReactNode }) {
  const insets = useSafeAreaInsets();
  const connectivity = useConnectivity();
  const connectivityLost = isOffline(connectivity);
  const [showOffline, setShowOffline] = useState(false);

  // A Wi-Fi ↔ cellular handoff can report a disconnected frame before the
  // next network is ready. Only show the strip once the outage is real.
  useEffect(() => {
    if (!connectivityLost) {
      const hideTimer = setTimeout(() => setShowOffline(false), 0);
      return () => clearTimeout(hideTimer);
    }
    const timer = setTimeout(() => setShowOffline(true), 500);
    return () => clearTimeout(timer);
  }, [connectivityLost]);

  return (
    <View style={{ flex: 1 }}>
      {showOffline ? <OfflineBanner topInset={insets.top} /> : null}
      <SafeAreaInsetsContext.Provider
        value={showOffline ? { ...insets, top: 0 } : insets}
      >
        <View style={{ flex: 1 }}>{children}</View>
      </SafeAreaInsetsContext.Provider>
    </View>
  );
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
                  <SessionActivityBoundary>
                    <LightboxProvider>
                      <StatusBar style={isDarkColorScheme ? "light" : "dark"} />
                      <NetworkRefreshBridge />
                      <AppChrome>
                        <RootErrorBoundary>
                          <Stack screenOptions={{ headerShown: false }}>
                            <Stack.Screen name="index" />
                            <Stack.Screen name="(auth)" />
                            <Stack.Screen name="(app)" />
                          </Stack>
                        </RootErrorBoundary>
                        <PortalHost />
                      </AppChrome>
                    </LightboxProvider>
                  </SessionActivityBoundary>
                </AuthInitializer>
              </ThemeProvider>
            </QueryClientProvider>
          </KeyboardProvider>
        </SafeAreaProvider>
      </MobileI18nProvider>
    </GestureHandlerRootView>
  );
}

/**
 * Expo Router's route-level boundary. A failure inside any route renders the
 * same recoverable screen instead of a blank window; the id is stable and
 * copyable for a bug report.
 */
export function ErrorBoundary(props: ErrorBoundaryProps) {
  return <RouteErrorBoundary {...props} />;
}
