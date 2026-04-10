import { CoreProvider } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { ThemeProvider } from "@multica/ui/components/common/theme-provider";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Toaster } from "sonner";
import { DesktopLoginPage } from "./pages/login";
import { DesktopShell } from "./components/desktop-layout";

function AppContent() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

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

export default function App() {
  return (
    <ThemeProvider>
      <CoreProvider
        apiBaseUrl={import.meta.env.VITE_API_URL || "http://localhost:8080"}
        wsUrl={import.meta.env.VITE_WS_URL || "ws://localhost:8080/ws"}
      >
        <AppContent />
      </CoreProvider>
      <Toaster />
    </ThemeProvider>
  );
}
