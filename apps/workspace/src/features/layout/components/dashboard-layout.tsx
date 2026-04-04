"use client";

import { useEffect } from "react";
import { MulticaIcon } from "@/components/multica-icon";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";
import { useAuthStore } from "@/features/auth";
import { useNavigationStore } from "@/features/navigation";
import { useWorkspaceStore } from "@/features/workspace";
import { usePathname, useRouter } from "@/shared/router";
import { AppSidebar } from "./app-sidebar";

export function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const workspace = useWorkspaceStore((s) => s.workspace);

  useEffect(() => {
    if (!isLoading && !user && pathname !== "/login") {
      router.replace(`/login?next=${encodeURIComponent(pathname)}`);
    }
  }, [isLoading, pathname, router, user]);

  useEffect(() => {
    useNavigationStore.getState().onPathChange(pathname);
  }, [pathname]);

  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <MulticaIcon className="size-6" />
      </div>
    );
  }

  if (!user) return null;

  return (
    <SidebarProvider className="h-svh">
      <AppSidebar />
      <SidebarInset className="overflow-hidden">
        {workspace ? (
          children
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <MulticaIcon className="size-6 animate-pulse" />
          </div>
        )}
      </SidebarInset>
    </SidebarProvider>
  );
}
