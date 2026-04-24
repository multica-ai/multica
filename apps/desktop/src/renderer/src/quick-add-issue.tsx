import { useEffect, useState, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { CoreProvider, setCurrentWorkspace } from "@multica/core/platform";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { NavigationProvider, type NavigationAdapter } from "@multica/views/navigation";
import { ThemeProvider } from "@multica/ui/components/common/theme-provider";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { Toaster } from "sonner";
import { CreateIssueModal } from "@multica/views/modals/create-issue";
import { useIssueDraftStore } from "@multica/core/issues/stores/draft-store";

const NOOP_NAVIGATION: NavigationAdapter = {
  push() {},
  replace() {},
  back() {},
  pathname: "/",
  searchParams: new URLSearchParams(),
};

function QuickAddContent() {
  useEffect(() => {
    document.documentElement.setAttribute("data-quick-add", "true");
  }, []);

  const slug = new URLSearchParams(window.location.search).get("workspace");
  const { data: workspaces = [], isLoading } = useQuery(workspaceListOptions());
  const workspace = workspaces.find((w) => w.slug === slug);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    function trackFileDialog(e: MouseEvent) {
      const el = e.target as HTMLElement;
      if (el.tagName === "INPUT" && (el as HTMLInputElement).type === "file") {
        (window as any).__quickAddFileDialog = true;
      }
    }

    function clearFileDialog() {
      (window as any).__quickAddFileDialog = false;
    }

    document.addEventListener("click", trackFileDialog, true);
    window.addEventListener("focus", clearFileDialog);

    return () => {
      document.removeEventListener("click", trackFileDialog, true);
      window.removeEventListener("focus", clearFileDialog);
      delete (window as any).__quickAddFileDialog;
    };
  }, []);

  useEffect(() => {
    if (!workspace) return;

    setCurrentWorkspace(workspace.slug, workspace.id);

    async function initDraft() {
      try {
        await useIssueDraftStore.persist.rehydrate();
      } catch {
        // Rehydrate failed (storage missing or corrupt) — still show the form
      }
      setReady(true);
    }

    initDraft();
  }, [workspace]);

  const handleExpandChange = useCallback((expanded: boolean) => {
    const size = expanded
      ? { width: 900, height: 650 }
      : { width: 640, height: 420 };
    window.quickAddAPI.setSize(size.width, size.height);
  }, []);

  function handleClose() {
    window.quickAddAPI.close();
  }

  if (isLoading || !workspace || !ready) {
    return (
      <div className="flex h-screen w-screen items-center justify-center bg-transparent">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  return (
    <WorkspaceSlugProvider slug={slug}>
      <NavigationProvider value={NOOP_NAVIGATION}>
        <div className="h-screen w-screen bg-transparent">
          <CreateIssueModal onClose={handleClose} onExpandChange={handleExpandChange} />
        </div>
      </NavigationProvider>
    </WorkspaceSlugProvider>
  );
}

export function QuickAddIssue() {
  return (
    <ThemeProvider>
      <CoreProvider
        apiBaseUrl={import.meta.env.VITE_API_URL || "http://localhost:8080"}
        wsUrl={import.meta.env.VITE_WS_URL || "ws://localhost:8080/ws"}
        onLogout={() => {}}
        identity={{ platform: "desktop", version: "unknown", os: "unknown" }}
      >
        <QuickAddContent />
      </CoreProvider>
      <Toaster />
    </ThemeProvider>
  );
}
