import { Outlet } from "@tanstack/react-router";
import { ThemeProvider } from "@/components/theme-provider";
import { Toaster } from "@/components/ui/sonner";
import { AuthInitializer } from "@/features/auth";
import { ModalRegistry } from "@/features/modals";
import { WSProvider } from "@/features/realtime";
import { QueryProvider } from "@/shared/query";

export function AppShell() {
  return (
    <div className="h-full antialiased font-sans">
      <ThemeProvider>
        <QueryProvider>
          <AuthInitializer>
            <WSProvider>
              <Outlet />
            </WSProvider>
          </AuthInitializer>
          <ModalRegistry />
          <Toaster />
        </QueryProvider>
      </ThemeProvider>
    </div>
  );
}
