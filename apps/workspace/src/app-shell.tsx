import { Outlet } from "@tanstack/react-router";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/sonner";
import { AuthInitializer } from "@/features/auth";
import { ModalRegistry } from "@/features/modals";
import { WSProvider } from "@/features/realtime";

export function AppShell() {
  return (
    <div className="h-full antialiased font-sans">
      <ThemeProvider>
        <AuthInitializer>
          <WSProvider>
            <Outlet />
          </WSProvider>
        </AuthInitializer>
        <ModalRegistry />
        <Toaster />
      </ThemeProvider>
    </div>
  );
}
