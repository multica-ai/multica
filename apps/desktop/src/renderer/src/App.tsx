import { useEffect } from "react";
import { CoreProvider } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceStore } from "@multica/core/workspace";
import { api } from "@multica/core/api";
import { ThemeProvider } from "@multica/ui/components/common/theme-provider";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Toaster } from "sonner";
import { DesktopLoginPage } from "./pages/login";
import { DesktopShell } from "./components/desktop-layout";
import { UpdateNotification } from "./components/update-notification";

function AppContent() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  // Listen for auth token delivered via deep link (multica://auth/callback?token=...)
  useEffect(() => {
    return window.desktopAPI.onAuthToken(async (token) => {
      try {
        await useAuthStore.getState().loginWithToken(token);
        const wsList = await api.listWorkspaces();
        const lastWsId = localStorage.getItem("multica_workspace_id");
        useWorkspaceStore.getState().hydrateWorkspace(wsList, lastWsId);
      } catch {
        // Token invalid or expired — user stays on login page
      }
    });
  }, []);

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  if (!user) return <DesktopLoginPage />;
  return <DesktopShell />;
}

const remoteProxy = Boolean(import.meta.env.VITE_REMOTE_API);

export default function App() {
  return (
    <ThemeProvider>
      <CoreProvider
        apiBaseUrl={remoteProxy ? "" : (import.meta.env.VITE_API_URL || "http://localhost:8080")}
        wsUrl={remoteProxy ? "ws://localhost:5173/ws" : (import.meta.env.VITE_WS_URL || "ws://localhost:8080/ws")}
      >
        <AppContent />
      </CoreProvider>
      <Toaster />
      <UpdateNotification />
    </ThemeProvider>
  );
}
