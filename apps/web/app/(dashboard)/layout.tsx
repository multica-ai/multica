"use client";

import { useEffect } from "react";
import { useRouter, usePathname } from "next/navigation";
import { MulticaIcon } from "@/components/multica-icon";
import { useNavigationStore } from "@multica/core/navigation";
import { SidebarProvider, SidebarInset } from "@multica/ui/components/ui/sidebar";
import { useAuthStore } from "@/platform/auth";
import { useWorkspaceStore } from "@/platform/workspace";
import { WorkspaceIdProvider } from "@multica/core/hooks";
import { ModalRegistry } from "@multica/views/modals/registry";
import { SearchCommand } from "@/features/search";
import { DashboardLocaleProvider } from "@/features/dashboard/i18n";
import { AppSidebar } from "./_components/app-sidebar";

export default function DashboardLayout({
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
    if (!isLoading && !user) {
      router.push("/");
    }
  }, [user, isLoading, router]);

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

  // AppSidebar (and other dashboard children) call hooks that depend on the
  // workspace id via useWorkspaceId(), so the entire dashboard tree must
  // mount inside WorkspaceIdProvider. Show a fullscreen loader while the
  // workspace is still being resolved.
  if (!workspace) {
    return (
      <div className="flex h-svh items-center justify-center">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  return (
    <DashboardLocaleProvider>
      <WorkspaceIdProvider wsId={workspace.id}>
        <SidebarProvider className="h-svh">
          <AppSidebar />
          <SidebarInset className="overflow-hidden">
            {children}
            <ModalRegistry />
          </SidebarInset>
          <SearchCommand />
        </SidebarProvider>
      </WorkspaceIdProvider>
    </DashboardLocaleProvider>
  );
}
