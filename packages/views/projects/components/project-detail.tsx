"use client";

import { useMemo, useState, useCallback, useRef, useEffect } from "react";
import { useRegisterCommandBarContext } from "../../search/register-context";
import { useDefaultLayout, usePanelRef } from "react-resizable-panels";
import { Archive, ArchiveRestore, BellOff, BellRing, FileQuestion, Link2, PanelRight, Pencil, Pin, PinOff, Trash2, Type } from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { copyText, copyTextAndHtml } from "@multica/ui/lib/clipboard";
import { toast } from "sonner";
import { isFeatureLockedError } from "@multica/core/billing";
import type { ProjectStatus } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { canDeleteProject } from "@multica/core/projects/deletion-permissions";
import { projectDetailOptions, projectSubscriptionOptions } from "@multica/core/projects/queries";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useUpdateProject, useDeleteProject, useSetProjectSubscription, ProjectDeletionOfflineError } from "@multica/core/projects/mutations";
import { pinListOptions } from "@multica/core/pins";
import { useCreatePin, useDeletePin } from "@multica/core/pins";
import { useModalStore } from "@multica/core/modals";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssuesScope } from "@multica/core/issues/stores";
import { useRecentContextStore } from "@multica/core/chat";
import { useWorkspacePaths } from "@multica/core/paths";
import type { BreakdownFilter } from "./project-progress-section";
import { ProjectInsights } from "./project-insights";
import { ProjectPropertiesPanel } from "./project-properties";
import { ProjectActivityPage } from "./project-activity";
import { getIssueSurfaceViewStore, useIssuesScopeStore } from "@multica/core/issues/stores";
import { issueScopeKey } from "@multica/core/issues/surface/scope";
import { ProjectOverviewBody, ProjectOverviewHeader, ProjectTabs } from "./project-overview";
import {
  defaultProjectTab,
  isProjectTab,
  useProjectTabStore,
  type ProjectTab,
} from "@multica/core/projects/tab-store";
import { AppLink, currentPath, useNavigation } from "../../navigation";
import { usePaidFeatureErrorMessage } from "../../billing";
import { ProjectWorkflowMenuItem, ProjectWorkflowSheet } from "../../workflows/project-workflow-sheet";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import { Sheet, SheetContent } from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { IssueViewHeader } from "../../issue-views/view-header";
import {
  AnimatedRightSidebar,
  getAnimatedRightSidebarInitialOpen,
  rightSidebarPanelMotionProps,
  useAnimatedRightSidebarState,
  useRightSidebarShortcut,
} from "../../layout/animated-right-sidebar";
import { CollectionPageState } from "../../layout/collection-page";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useT } from "../../i18n";
import { titleLinkPayload } from "../../common/title-link";
import {
  ArchivedProjectNotice,
  ArchivedProjectTag,
  useArchiveProjects,
  useOfferArchiveOnComplete,
  useRestoreProjects,
} from "./project-archive";
import { PAGE_GUTTER } from "../../layout/page-header";

// ProjectDetail

export function ProjectDetail({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const userId = useAuthStore((s) => s.user?.id);
  const deleteProject = useDeleteProject();
  const deleted = deleteProject.isSuccess && deleteProject.variables === projectId;
  const { requestArchive, dialog: archiveDialog } = useArchiveProjects();
  const restore = useRestoreProjects();
  const offerArchive = useOfferArchiveOnComplete();
  const { data: project, isLoading } = useQuery({
    ...projectDetailOptions(wsId, projectId),
    enabled: !deleted,
  });
  useRegisterCommandBarContext(
    useMemo(() => (project ? { kind: "project", project } : null), [project]),
  );
  const recordRecentContext = useRecentContextStore((s) => s.recordVisit);
  useEffect(() => {
    if (project) {
      recordRecentContext(wsId, {
        type: "project",
        id: project.id,
        label: project.title,
        subtitle: project.description ?? undefined,
        icon: project.icon,
        projectStatus: project.status,
      });
    }
  }, [project?.id, project?.title, project?.description, project?.icon, project?.status, recordRecentContext, wsId]);
  const issueTab = useIssuesScope(`project:${projectId}`);
  const issueScope = useMemo(
    () => ({ type: "project" as const, projectId, actorKind: issueTab }),
    [projectId, issueTab],
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const updateProject = useUpdateProject();
  const paidFeatureError = usePaidFeatureErrorMessage();
  const deleting = deleteProject.isPending;
  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });
  const isPinned = pinnedItems.some((p) => p.item_type === "project" && p.item_id === projectId);
  const role = members.find((m) => m.user_id === userId)?.role ?? null;
  const canDelete = !!project && canDeleteProject(project, userId ?? null, role, agents);
  const createPin = useCreatePin();
  const deletePinMut = useDeletePin();
  const { data: isSubscribed = false } = useQuery({
    ...projectSubscriptionOptions(wsId, projectId),
    enabled: !!project,
  });
  const setSubscription = useSetProjectSubscription();
  const handleToggleSubscription = () => {
    setSubscription.mutate(
      { id: projectId, subscribed: !isSubscribed },
      {
        onSuccess: (subscribed) => {
          if (subscribed) toast.success(t(($) => $.detail.toast_subscribed));
        },
        onError: () => toast.error(t(($) => $.detail.toast_subscription_failed)),
      },
    );
  };
  const isMobile = useIsMobile();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  // Overview / Issues. An explicit ?tab= wins and is remembered; otherwise the
  // project opens on the tab this person last left it on.
  const tabParam = router.searchParams.get("tab");
  const rememberedTab = useProjectTabStore((s) => s.lastTab[projectId]);
  const rememberTab = useProjectTabStore((s) => s.remember);
  const tab: ProjectTab = isProjectTab(tabParam)
    ? tabParam
    : defaultProjectTab(rememberedTab, project?.issue_count ?? 0);
  useEffect(() => {
    if (isProjectTab(tabParam)) rememberTab(projectId, tabParam);
  }, [projectId, tabParam, rememberTab]);
  const handleTabChange = useCallback(
    (next: ProjectTab) => {
      rememberTab(projectId, next);
      const params = new URLSearchParams(router.searchParams);
      params.set("tab", next);
      router.replace(`${router.pathname}?${params.toString()}`);
    },
    [projectId, rememberTab, router],
  );

  // Sidebar panel
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_project_detail_layout",
  });
  const sidebarRef = usePanelRef();
  const rightSidebarShortcutTargetRef = useRef<HTMLDivElement | null>(null);
  const desktopSidebarInitialOpen = getAnimatedRightSidebarInitialOpen(
    true,
    defaultLayout,
  );
  // Desktop and mobile sidebar state must be separate. A single state defaulting
  // to `true` made the mobile <Sheet> mount in the open position on first render
  // (after `useIsMobile()` flipped from false→true), briefly covering the page
  // with its modal backdrop and locking scroll — leaving the page unresponsive.
  const {
    open: desktopSidebarOpen,
    visualOpen: desktopSidebarVisualOpen,
    motionEnabled: desktopSidebarMotionEnabled,
    beginToggle: beginDesktopSidebarToggle,
    handleResize: handleDesktopSidebarResize,
  } = useAnimatedRightSidebarState(desktopSidebarInitialOpen);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const sidebarOpen = isMobile ? mobileSidebarOpen : desktopSidebarOpen;
  const [workflowSheetOpen, setWorkflowSheetOpen] = useState(false);
  // The flow panel is its own modal sheet; it can't stack over the mobile sidebar sheet.
  const openWorkflowSheet = useCallback(() => {
    setMobileSidebarOpen(false);
    setWorkflowSheetOpen(true);
  }, []);

  useEffect(() => {
    if (isMobile) {
      setMobileSidebarOpen(false);
    }
  }, [isMobile]);

  const handleToggleSidebar = useCallback(() => {
    if (isMobile) {
      setMobileSidebarOpen((open) => !open);
      return;
    }

    const panel = sidebarRef.current;
    if (!panel) return;
    const nextOpen = panel.isCollapsed();
    beginDesktopSidebarToggle(nextOpen);
    window.requestAnimationFrame(() => {
      if (nextOpen) panel.expand();
      else panel.collapse();
    });
  }, [beginDesktopSidebarToggle, isMobile, sidebarRef]);

  useRightSidebarShortcut(rightSidebarShortcutTargetRef, handleToggleSidebar);

  // Lead popover

  const handleUpdateField = useCallback(
    (data: Parameters<typeof updateProject.mutate>[0] extends { id: string } & infer R ? R : never) => {
      if (!project) return;
      // Field edits roll back silently; a plan refusal (a lead who would join
      // a private project) needs saying why.
      updateProject.mutateAsync({ id: project.id, ...data }).catch((err: unknown) => {
        if (isFeatureLockedError(err)) toast.error(paidFeatureError(err, ""));
      });
    },
    [project, updateProject, paidFeatureError],
  );

  const handleStatusChange = useCallback(
    (status: ProjectStatus) => {
      if (!project) return;
      handleUpdateField({ status });
      offerArchive(project, status);
    },
    [project, handleUpdateField, offerArchive],
  );

  // A breakdown row opens the Issues tab showing just that slice.
  const showFilteredIssues = useCallback(
    (filter: BreakdownFilter) => {
      // Any slice lands on the All tab: Members or Agents would hide half of
      // them, and each tab keeps its own filters.
      useIssuesScopeStore.getState().setScope(`project:${projectId}`, "all");
      const store = getIssueSurfaceViewStore(issueScopeKey({ type: "project", projectId, actorKind: "all" }));
      store.getState().clearFilters();
      if (filter.kind === "assignee") {
        store.setState({ assigneeFilters: [{ type: filter.type, id: filter.id }] });
      } else if (filter.kind === "unassigned") {
        store.setState({ includeNoAssignee: true });
      } else {
        store.setState({ labelFilters: [filter.id] });
      }
      setMobileSidebarOpen(false);
      handleTabChange("issues");
    },
    [projectId, handleTabChange],
  );

  const handleDelete = useCallback(() => {
    if (!project || deleteProject.isPending) return;
    deleteProject.mutate(project.id, {
      onSuccess: () => {
        setDeleteDialogOpen(false);
        toast.success(t(($) => $.detail.toast_project_deleted));
        router.push(wsPaths.projects());
      },
      onError: (err) =>
        toast.error(
          err instanceof ProjectDeletionOfflineError
            ? t(($) => $.delete_dialog.offline_error)
            : err instanceof Error ? err.message : String(err),
        ),
    });
  }, [project, deleteProject, router, wsPaths, t]);

  if (isLoading || deleted) {
    return (
      <div className="mx-auto w-full max-w-4xl px-8 py-10 space-y-4">
        <Skeleton className="h-5 w-32" />
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-4 w-96" />
        <Skeleton className="h-40 w-full mt-8" />
      </div>
    );
  }

  if (!project) {
    return (
      <div className="flex h-full flex-col">
        <CollectionPageState
          icon={FileQuestion}
          title={t(($) => $.detail.not_found)}
          description={t(($) => $.detail.not_found_description)}
          actions={
            <AppLink
              href={wsPaths.projects()}
              className={buttonVariants({ variant: "outline", size: "sm" })}
            >
              {t(($) => $.detail.back_to_list)}
            </AppLink>
          }
        />
      </div>
    );
  }

  const archived = !!project.archived_at;

  const sidebarContent = (
    <div className="space-y-5">
      <ProjectPropertiesPanel
        project={project}
        onUpdate={handleUpdateField}
        onStatusChange={handleStatusChange}
        onOpenWorkflow={openWorkflowSheet}
      />
      <ProjectInsights
        project={project}
        onFilter={showFilteredIssues}
        onShowActivity={() => {
          setMobileSidebarOpen(false);
          handleTabChange("activity");
        }}
      />
    </div>
  );

  const projectMenu = (
    <>
      <DropdownMenuItem
        onClick={() =>
          useModalStore
            .getState()
            .open("project-rename", { projectId: project.id, title: project.title })
        }
      >
        <Pencil className="h-3.5 w-3.5" />
        {t(($) => $.detail.rename_action)}
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => {
        void copyText(router.getShareableUrl(currentPath(router))).then((ok) => {
          if (ok) toast.success(t(($) => $.detail.toast_link_copied));
        });
      }}>
        <Link2 className="h-3.5 w-3.5" />
        {t(($) => $.detail.copy_link)}
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => {
        const { text, html } = titleLinkPayload(
          project.title,
          router.getShareableUrl(currentPath(router)),
        );
        void copyTextAndHtml(text, html).then((ok) => {
          if (ok) {
            toast.success(t(($) => $.detail.toast_title_link_copied));
          } else {
            toast.error(t(($) => $.detail.toast_title_link_copy_failed));
          }
        });
      }}>
        <Type className="h-3.5 w-3.5" />
        {t(($) => $.detail.copy_title_as_link)}
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => {
        if (isPinned) {
          deletePinMut.mutate({ itemType: "project", itemId: projectId });
        } else {
          createPin.mutate({ item_type: "project", item_id: projectId });
        }
      }}>
        {isPinned ? <PinOff className="h-3.5 w-3.5" /> : <Pin className="h-3.5 w-3.5" />}
        {isPinned ? t(($) => $.detail.unpin_tooltip) : t(($) => $.detail.pin_tooltip)}
      </DropdownMenuItem>
      <DropdownMenuItem
        disabled={setSubscription.isPending}
        onClick={handleToggleSubscription}
      >
        {isSubscribed ? <BellOff className="h-3.5 w-3.5" /> : <BellRing className="h-3.5 w-3.5" />}
        {isSubscribed ? t(($) => $.detail.unsubscribe_action) : t(($) => $.detail.subscribe_action)}
      </DropdownMenuItem>
      <ProjectWorkflowMenuItem project={project} onOpen={() => setWorkflowSheetOpen(true)} />
      <DropdownMenuSeparator />
      {archived ? (
        <DropdownMenuItem onClick={() => restore(project)}>
          <ArchiveRestore className="h-3.5 w-3.5" />
          {t(($) => $.detail.restore_action)}
        </DropdownMenuItem>
      ) : (
        <DropdownMenuItem onClick={() => requestArchive([project])}>
          <Archive className="h-3.5 w-3.5" />
          {t(($) => $.detail.archive_action)}
        </DropdownMenuItem>
      )}
      {canDelete && (
        <DropdownMenuItem
          variant="destructive"
          disabled={deleting}
          aria-busy={deleting}
          onClick={() => setDeleteDialogOpen(true)}
        >
          {deleting ? <Spinner className="size-3.5" aria-hidden="true" /> : <Trash2 className="h-3.5 w-3.5" />}
          {t(($) => $.detail.delete_action)}
        </DropdownMenuItem>
      )}
    </>
  );

  const sidebarToggle = (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant={sidebarOpen ? "secondary" : "ghost"}
            size="icon-sm"
            className={sidebarOpen ? "" : "text-muted-foreground"}
            aria-label={t(($) => $.detail.sidebar_tooltip)}
            onClick={handleToggleSidebar}
          >
            <PanelRight />
          </Button>
        }
      />
      <TooltipContent side="bottom">{t(($) => $.detail.sidebar_tooltip)}</TooltipContent>
    </Tooltip>
  );

  const projectTabs = (
    <ProjectTabs tab={tab} onTabChange={handleTabChange} issueCount={project.issue_count} />
  );

  return (
    <>
    <ResizablePanelGroup
      data-mc="object-page"
      data-type="project"
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel id="content" minSize="50%">
        <div ref={rightSidebarShortcutTargetRef} className="flex h-full flex-col">
          {tab !== "issues" ? (
            <>
              <ProjectOverviewHeader
                project={project}
                tabs={projectTabs}
                menu={projectMenu}
                badge={archived ? <ArchivedProjectTag /> : undefined}
                trailing={sidebarToggle}
              />
              {archived && (
                <div className={cn("shrink-0 pt-2", PAGE_GUTTER)}>
                  <ArchivedProjectNotice project={project} />
                </div>
              )}
              {tab === "overview" ? (
                <ProjectOverviewBody project={project} onUpdate={handleUpdateField} />
              ) : (
                <ProjectActivityPage projectId={project.id} />
              )}
            </>
          ) : (
            <IssueSurface
              scope={issueScope}
              modes={["board", "list", "table", "swimlane", "gantt"]}
              allowCreate={!archived}
              renderHeader={({ controller }) => (
                <>
                  <IssueViewHeader
                    controller={controller}
                    base={{ kind: "project", project }}
                    baseBadge={archived ? <ArchivedProjectTag /> : undefined}
                    hideCreate={archived}
                    afterTitle={projectTabs}
                    baseMenu={projectMenu}
                    trailing={sidebarToggle}
                  />
                  {archived && (
                    <div className={cn("shrink-0 pt-2", PAGE_GUTTER)}>
                      <ArchivedProjectNotice project={project} />
                    </div>
                  )}
                </>
              )}
            />
          )}
          </div>
        </ResizablePanel>
        {!isMobile && <ResizableHandle />}
        {!isMobile && (
        <ResizablePanel
          id="sidebar"
          {...rightSidebarPanelMotionProps}
          data-right-sidebar-motion={desktopSidebarMotionEnabled ? "enabled" : undefined}
          defaultSize={desktopSidebarOpen ? 320 : 0}
          minSize={260}
          maxSize={420}
          collapsible
          groupResizeBehavior="preserve-pixel-size"
          panelRef={sidebarRef}
          onResize={handleDesktopSidebarResize}
        >
          <AnimatedRightSidebar open={desktopSidebarVisualOpen} motionEnabled={desktopSidebarMotionEnabled}>
            {sidebarContent}
          </AnimatedRightSidebar>
        </ResizablePanel>
        )}
        {isMobile && (
          <Sheet open={mobileSidebarOpen} onOpenChange={setMobileSidebarOpen}>
            <SheetContent side="right" showCloseButton={false} className="w-[320px] overflow-y-auto p-4">
              {sidebarContent}
            </SheetContent>
          </Sheet>
        )}
      </ResizablePanelGroup>

      <ProjectWorkflowSheet project={project} open={workflowSheetOpen} onOpenChange={setWorkflowSheetOpen} />
      {archiveDialog}

      {/* Delete confirmation */}
      {canDelete && (
        <AlertDialog
          open={deleteDialogOpen}
          onOpenChange={(open) => {
            if (!deleting) setDeleteDialogOpen(open);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t(($) => $.delete_dialog.title)}</AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.delete_dialog.description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            {!archived && (
              <div className="flex items-start gap-2 rounded-lg border bg-muted/40 px-3 py-2 text-caption text-muted-foreground">
                <Archive className="mt-0.5 size-3.5 shrink-0" />
                <span>{t(($) => $.delete_dialog.archive_hint)}</span>
              </div>
            )}
            <AlertDialogFooter>
              {!archived && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="sm:mr-auto"
                  disabled={deleting}
                  onClick={() => {
                    setDeleteDialogOpen(false);
                    requestArchive([project], { immediate: true });
                  }}
                >
                  <Archive className="size-3.5" />
                  {t(($) => $.delete_dialog.archive_instead)}
                </Button>
              )}
              <AlertDialogCancel disabled={deleting}>
                {t(($) => $.delete_dialog.cancel)}
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={deleting}
                aria-busy={deleting}
                onClick={handleDelete}
                className="bg-destructive text-white hover:bg-destructive/90"
              >
                {deleting && <Spinner className="size-3.5" aria-hidden="true" />}
                {t(($) => $.delete_dialog.confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      )}
    </>
  );
}
