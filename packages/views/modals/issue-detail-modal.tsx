"use client";

import { useEffect, useRef, useMemo } from "react";
import { useModalStore } from "@multica/core/modals";
import { Dialog, DialogContent } from "@multica/ui/components/ui/dialog";
import { IssueDetail } from "../issues/components/issue-detail";
import { useNavigation, NavigationProvider } from "../navigation";
import { useWorkspacePaths } from "@multica/core/paths";

function getIssueIdFromPath(path: string): string | null {
  const parts = path.split("/").filter(Boolean);
  const issuesIndex = parts.indexOf("issues");
  if (issuesIndex !== -1) {
    return parts[issuesIndex + 1] ?? null;
  }
  return null;
}

export function IssueDetailModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const issueId = (data?.issueId as string) || "";
  const navigation = useNavigation();
  const paths = useWorkspacePaths();

  // Keep track of initial path before opening the modal so we can restore it on close.
  const initialPathname = useRef(
    navigation.pathname + (navigation.searchParams.toString() ? `?${navigation.searchParams.toString()}` : "")
  );

  const initialRouterPathname = useRef(navigation.pathname);

  const shouldRestoreUrl = useRef(true);

  const issueUrl = paths.issueDetail(issueId);

  useEffect(() => {
    if (issueId) {
      window.history.pushState(null, "", issueUrl);
    }

    return () => {
      if (shouldRestoreUrl.current) {
        window.history.pushState(null, "", initialPathname.current);
      }
    };
  }, [issueId, issueUrl]);

  useEffect(() => {
    // If a real navigation occurred and changed the router's active pathname to any other page
    // (outside of the /slug/issues/ subpath), we must close the modal and skip restoring the old URL.
    if (
      navigation.pathname !== initialRouterPathname.current &&
      !navigation.pathname.startsWith(paths.issues() + "/")
    ) {
      shouldRestoreUrl.current = false;
      onClose();
    }
  }, [navigation.pathname, paths, onClose]);

  useEffect(() => {
    const handlePopState = () => {
      shouldRestoreUrl.current = false;
      onClose();
    };

    window.addEventListener("popstate", handlePopState);
    return () => {
      window.removeEventListener("popstate", handlePopState);
    };
  }, [onClose]);

  const customNavigation = useMemo(() => {
    return {
      ...navigation,
      push: (path: string) => {
        const nextId = getIssueIdFromPath(path);
        if (nextId) {
          useModalStore.getState().open("issue-detail", { issueId: nextId });
        } else {
          navigation.push(path);
        }
      },
      replace: (path: string) => {
        const nextId = getIssueIdFromPath(path);
        if (nextId) {
          useModalStore.getState().open("issue-detail", { issueId: nextId });
        } else {
          navigation.replace(path);
        }
      },
    };
  }, [navigation]);

  if (!issueId) return null;

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className="p-0 gap-0 flex flex-col overflow-hidden !max-w-6xl !w-full !h-[85vh] !top-1/2 !left-1/2 !-translate-x-1/2 !-translate-y-1/2 !transition-all !duration-300 !ease-out"
      >
        <div className="flex-1 min-h-0 overflow-hidden">
          <NavigationProvider value={customNavigation}>
            <IssueDetail
              issueId={issueId}
              onDelete={onClose}
              onDone={onClose}
              defaultSidebarOpen={true}
              showMaximize={true}
              onMaximize={() => {
                shouldRestoreUrl.current = false;
                onClose();
                navigation.push(paths.issueDetail(issueId));
              }}
            />
          </NavigationProvider>
        </div>
      </DialogContent>
    </Dialog>
  );
}
