"use client";

import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import {
  BookmarkPlus,
  Check,
  ChevronDown,
  Copy,
  CopyPlus,
  Ellipsis,
  LayoutTemplate,
  LockKeyhole,
  Pencil,
  Pin,
  PinOff,
  RotateCcw,
  Save,
  Star,
  StarOff,
  Trash2,
  Users,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { copyText } from "@multica/ui/lib/clipboard";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueViewListOptions,
} from "@multica/core/issue-views/queries";
import {
  defaultIssueViewRequest,
  useCreateIssueView,
  useDeleteIssueView,
  useDuplicateIssueView,
  useSetDefaultIssueView,
  useUpdateIssueView,
} from "@multica/core/issue-views/mutations";
import {
  applyIssueViewTemplate,
  ISSUE_VIEW_TEMPLATE_IDS,
  type IssueViewTemplateId,
} from "@multica/core/issue-views/templates";
import {
  issueViewDefinitionFromState,
  issueViewDefinitionsEqual,
  type IssueViewDefinition,
  type IssueViewDefinitionContext,
} from "@multica/core/issues/stores/view-store";
import {
  applyIssueSurfaceSavedView,
  restoreIssueSurfaceDraft,
} from "@multica/core/issues/stores/surface-view-store";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import type { IssueScope } from "@multica/core/issues/surface/scope";
import { pinListOptions } from "@multica/core/pins/queries";
import { useCreatePin, useDeletePin } from "@multica/core/pins/mutations";
import type {
  IssueView,
  IssueViewScopeInput,
  IssueViewVisibility,
} from "@multica/core/types";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

const EMPTY_VIEWS: IssueView[] = [];
const VIEW_STRIP_GAP = 4;

/**
 * Pick the saved-view tabs that fit beside the stable More trigger and the
 * active-view actions. The active view is admitted first, then the normal
 * pinned/position order fills the remaining space. Returning IDs in the
 * original order keeps tab positions stable even when an overflow item is
 * temporarily promoted into view.
 */
export function visibleSavedViewIDs(
  views: readonly IssueView[],
  activeViewID: string | null,
  widths: ReadonlyMap<string, number>,
  availableWidth: number,
  gap = VIEW_STRIP_GAP,
): string[] {
  if (views.length === 0) return [];
  if (availableWidth <= 0) {
    return activeViewID && views.some((view) => view.id === activeViewID)
      ? [activeViewID]
      : [];
  }

  const selected = new Set<string>();
  let used = 0;
  const add = (view: IssueView, force = false) => {
    const width = widths.get(view.id) ?? 0;
    const next = used + (selected.size > 0 ? gap : 0) + width;
    if (!force && next > availableWidth) return;
    selected.add(view.id);
    used = next;
  };

  const activeView = views.find((view) => view.id === activeViewID);
  if (activeView) add(activeView, true);
  for (const view of views) {
    if (!selected.has(view.id)) add(view);
  }

  return views
    .filter((view) => selected.has(view.id))
    .map((view) => view.id);
}

function apiScopeFromIssueScope(scope: IssueScope): IssueViewScopeInput | null {
  if (scope.type === "workspace") return { scope_type: "workspace" };
  if (scope.type === "project") {
    return { scope_type: "project", scope_id: scope.projectId };
  }
  if (scope.type === "my") return { scope_type: "my" };
  return null;
}

function contextFromIssueScope(scope: IssueScope): IssueViewDefinitionContext {
  if (scope.type === "workspace") {
    return { workspaceActorKind: scope.actorKind ?? "all" };
  }
  if (scope.type === "my") return { myRelation: scope.relation };
  return {};
}

function contextFromDefinition(
  definition: IssueViewDefinition,
  scope: IssueScope,
): IssueViewDefinitionContext {
  if (scope.type === "workspace") {
    return { workspaceActorKind: definition.workspaceActorKind ?? "all" };
  }
  if (scope.type === "my") {
    return { myRelation: definition.myRelation ?? "assigned" };
  }
  return {};
}

function pathWithView(
  pathname: string,
  searchParams: URLSearchParams,
  viewId: string | null,
) {
  const next = new URLSearchParams(searchParams);
  if (viewId) next.set("view", viewId);
  else next.delete("view");
  const query = next.toString();
  return query ? `${pathname}?${query}` : pathname;
}

interface SaveViewDialogProps {
  open: boolean;
  defaultName: string;
  isMyIssues: boolean;
  pending: boolean;
  onOpenChange: (open: boolean) => void;
  onSave: (
    name: string,
    visibility: IssueViewVisibility,
    template: IssueViewTemplateId,
  ) => void;
}

function SaveViewDialog({
  open,
  defaultName,
  isMyIssues,
  pending,
  onOpenChange,
  onSave,
}: SaveViewDialogProps) {
  const { t } = useT("issues");
  const [name, setName] = useState(defaultName);
  const [visibility, setVisibility] = useState<IssueViewVisibility>("private");
  const [template, setTemplate] = useState<IssueViewTemplateId>("current");

  useEffect(() => {
    if (!open) return;
    setName(defaultName);
    setVisibility("private");
    setTemplate("current");
  }, [defaultName, open]);

  const templateItems = ISSUE_VIEW_TEMPLATE_IDS.map((value) => ({
    value,
    label: t(($) => $.saved_views.templates[value]),
  }));
  const visibilityItems = [
    { value: "private" as const, label: t(($) => $.saved_views.private) },
    { value: "workspace" as const, label: t(($) => $.saved_views.workspace) },
  ];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.saved_views.save_dialog_title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.saved_views.save_dialog_description)}
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-1">
          <div className="grid gap-1.5">
            <Label htmlFor="saved-view-name">{t(($) => $.saved_views.name)}</Label>
            <Input
              id="saved-view-name"
              value={name}
              maxLength={80}
              autoFocus
              onChange={(event) => setName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && name.trim()) {
                  onSave(name.trim(), isMyIssues ? "private" : visibility, template);
                }
              }}
            />
          </div>
          <div className="grid gap-1.5">
            <Label>{t(($) => $.saved_views.template)}</Label>
            <Select
              items={templateItems}
              value={template}
              onValueChange={(value) => value && setTemplate(value)}
            >
              <SelectTrigger className="w-full">
                <LayoutTemplate className="size-3.5 text-muted-foreground" />
                <SelectValue />
              </SelectTrigger>
              <SelectContent align="start">
                {templateItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {!isMyIssues && (
            <div className="grid gap-1.5">
              <Label>{t(($) => $.saved_views.visibility)}</Label>
              <Select
                items={visibilityItems}
                value={visibility}
                onValueChange={(value) => value && setVisibility(value)}
              >
                <SelectTrigger className="w-full">
                  {visibility === "private" ? (
                    <LockKeyhole className="size-3.5 text-muted-foreground" />
                  ) : (
                    <Users className="size-3.5 text-muted-foreground" />
                  )}
                  <SelectValue />
                </SelectTrigger>
                <SelectContent align="start">
                  <SelectItem value="private">
                    <LockKeyhole className="size-3.5" />
                    {t(($) => $.saved_views.private)}
                  </SelectItem>
                  <SelectItem value="workspace">
                    <Users className="size-3.5" />
                    {t(($) => $.saved_views.workspace)}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t(($) => $.saved_views.cancel)}
          </Button>
          <Button
            disabled={!name.trim() || pending}
            onClick={() =>
              onSave(name.trim(), isMyIssues ? "private" : visibility, template)
            }
          >
            {t(($) => $.saved_views.save_view)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface RenameViewDialogProps {
  view: IssueView | null;
  pending: boolean;
  onClose: () => void;
  onRename: (name: string) => void;
}

function RenameViewDialog({ view, pending, onClose, onRename }: RenameViewDialogProps) {
  const { t } = useT("issues");
  const [name, setName] = useState("");
  useEffect(() => setName(view?.name ?? ""), [view]);
  return (
    <Dialog open={!!view} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.saved_views.rename_title)}</DialogTitle>
        </DialogHeader>
        <Input
          value={name}
          maxLength={80}
          autoFocus
          onChange={(event) => setName(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && name.trim()) onRename(name.trim());
          }}
        />
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t(($) => $.saved_views.cancel)}
          </Button>
          <Button disabled={!name.trim() || pending} onClick={() => onRename(name.trim())}>
            {t(($) => $.saved_views.rename)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export interface SavedViewsBarProps {
  scope: IssueScope;
  surfaceKey: string;
  onContextChange?: (context: IssueViewDefinitionContext) => void;
  children?: (context: SavedViewsRenderContext) => ReactNode;
}

export interface SavedViewsRenderContext {
  savedViewsControl: ReactNode;
  isSavedViewActive: boolean;
  selectBuiltInView: () => void;
}

export function SavedViewsBar({
  scope,
  surfaceKey,
  onContextChange,
  children,
}: SavedViewsBarProps) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((state) => state.user?.id ?? "");
  const navigation = useNavigation();
  const store = useViewStoreApi();
  const viewState = useViewStore((state) => state);
  const apiScope = useMemo(() => apiScopeFromIssueScope(scope), [scope]);
  const currentContext = useMemo(() => contextFromIssueScope(scope), [scope]);
  const currentDefinition = useMemo(
    () => issueViewDefinitionFromState(viewState, currentContext),
    [currentContext, viewState],
  );
  const query = useQuery({
    ...issueViewListOptions(
      wsId,
      apiScope ?? { scope_type: "workspace" },
    ),
    enabled: !!apiScope,
  });
  const views = query.data?.views ?? EMPTY_VIEWS;
  const urlViewId = navigation.searchParams.get("view");
  const activeView = views.find((view) => view.id === urlViewId) ?? null;
  const createView = useCreateIssueView();
  const updateView = useUpdateIssueView();
  const deleteView = useDeleteIssueView();
  const duplicateView = useDuplicateIssueView();
  const setDefault = useSetDefaultIssueView();
  const { data: pins = [] } = useQuery({
    ...pinListOptions(wsId, userId),
    enabled: !!userId,
  });
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const [saveDialogOpen, setSaveDialogOpen] = useState(false);
  const [saveAsName, setSaveAsName] = useState("");
  const [renameView, setRenameView] = useState<IssueView | null>(null);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const appliedViewRef = useRef<string | null>(null);
  const appliedDefinitionRef = useRef<IssueViewDefinition | null>(null);
  const draftContextRef = useRef<IssueViewDefinitionContext | null>(null);
  const pendingDirtyRestoreRef = useRef<{
    viewID: string;
    definition: IssueViewDefinition;
  } | null>(null);
  const onContextChangeRef = useRef(onContextChange);
  const suppressDefaultRef = useRef(false);
  const stripRef = useRef<HTMLDivElement | null>(null);
  const moreButtonRef = useRef<HTMLButtonElement | null>(null);
  const activeActionsRef = useRef<HTMLDivElement | null>(null);
  const tabRefs = useRef(new Map<string, HTMLButtonElement>());
  const pinnedViewIDs = useMemo(
    () =>
      new Set(
        pins
          .filter((pin) => pin.item_type === "view")
          .map((pin) => pin.item_id),
      ),
    [pins],
  );
  const orderedViews = useMemo(() => {
    const positions = new Map(views.map((view, index) => [view.id, index]));
    return [...views].sort((a, b) => {
      const pinOrder =
        Number(pinnedViewIDs.has(b.id)) - Number(pinnedViewIDs.has(a.id));
      if (pinOrder !== 0) return pinOrder;
      if (a.position !== b.position) return a.position - b.position;
      return (positions.get(a.id) ?? 0) - (positions.get(b.id) ?? 0);
    });
  }, [pinnedViewIDs, views]);
  const [visibleViewIDs, setVisibleViewIDs] = useState<string[] | null>(null);

  useEffect(() => {
    onContextChangeRef.current = onContextChange;
  }, [onContextChange]);

  // A saved view is only an overlay for this mounted route. Restore the
  // user's Default draft when navigating away so the surface store cannot
  // remain globally marked as a saved overlay after this component unmounts.
  useEffect(() => () => {
    if (!appliedViewRef.current) return;
    appliedViewRef.current = null;
    appliedDefinitionRef.current = null;
    restoreIssueSurfaceDraft(surfaceKey, store);
    if (draftContextRef.current) {
      onContextChangeRef.current?.(draftContextRef.current);
      draftContextRef.current = null;
    }
  }, [store, surfaceKey]);

  const navigateToView = useCallback(
    (viewId: string | null) => {
      navigation.replace(
        pathWithView(
          navigation.pathname,
          navigation.searchParams,
          viewId,
        ),
      );
    },
    [navigation],
  );

  const restoreDefault = useCallback(() => {
    suppressDefaultRef.current = true;
    appliedViewRef.current = null;
    appliedDefinitionRef.current = null;
    restoreIssueSurfaceDraft(surfaceKey, store);
    if (draftContextRef.current) onContextChange?.(draftContextRef.current);
    draftContextRef.current = null;
    navigateToView(null);
  }, [navigateToView, onContextChange, store, surfaceKey]);

  useEffect(() => {
    if (!apiScope || !query.data) return;
    if (!urlViewId) {
      if (appliedViewRef.current) {
        restoreDefault();
        return;
      }
      if (
        query.data.default_view_id &&
        !suppressDefaultRef.current &&
        !appliedViewRef.current
      ) {
        navigateToView(query.data.default_view_id);
      }
      return;
    }
    if (!activeView) {
      // Create / duplicate invalidates this list before navigating. Keep the
      // copied URL in place until that refetch can supply the new row.
      if (query.isFetching) return;
      // A deleted/private view may remain in a copied URL. Recover to Default
      // instead of leaving the surface in a half-applied state.
      restoreDefault();
      toast.error(t(($) => $.saved_views.not_found));
      return;
    }
    const revision = `${activeView.id}:${activeView.updated_at}`;
    const pendingDirtyRestore = pendingDirtyRestoreRef.current;
    if (pendingDirtyRestore?.viewID === activeView.id) {
      pendingDirtyRestoreRef.current = null;
      if (!draftContextRef.current) draftContextRef.current = currentContext;
      appliedViewRef.current = revision;
      appliedDefinitionRef.current = activeView.definition;
      onContextChange?.(
        contextFromDefinition(pendingDirtyRestore.definition, scope),
      );
      applyIssueSurfaceSavedView(
        surfaceKey,
        store,
        pendingDirtyRestore.definition,
      );
      return;
    }
    if (appliedViewRef.current === revision) return;
    const previousViewID = appliedViewRef.current?.split(":", 1)[0];
    const previousDefinition = appliedDefinitionRef.current;
    if (
      previousViewID === activeView.id &&
      previousDefinition &&
      !issueViewDefinitionsEqual(currentDefinition, previousDefinition)
    ) {
      // A rename, visibility change, or edit from another session must not
      // wipe local dirty work. Advance the server baseline and leave Reset to
      // apply the newly fetched definition explicitly.
      appliedViewRef.current = revision;
      appliedDefinitionRef.current = activeView.definition;
      return;
    }
    if (!draftContextRef.current) draftContextRef.current = currentContext;
    appliedViewRef.current = revision;
    appliedDefinitionRef.current = activeView.definition;
    onContextChange?.(contextFromDefinition(activeView.definition, scope));
    applyIssueSurfaceSavedView(surfaceKey, store, activeView.definition);
  }, [
    activeView,
    apiScope,
    currentContext,
    currentDefinition,
    navigateToView,
    onContextChange,
    query.data,
    query.isFetching,
    restoreDefault,
    scope,
    store,
    surfaceKey,
    t,
    urlViewId,
  ]);

  const dirty = !!activeView && !issueViewDefinitionsEqual(
    currentDefinition,
    activeView.definition,
  );
  const isDefault = !!activeView && query.data?.default_view_id === activeView.id;
  const isPinned = !!activeView && pins.some(
    (pin) => pin.item_type === "view" && pin.item_id === activeView.id,
  );
  const mutationPending =
    createView.isPending || updateView.isPending || duplicateView.isPending;
  const setViewTabRef = useCallback(
    (viewID: string, node: HTMLButtonElement | null) => {
      if (node) tabRefs.current.set(viewID, node);
      else tabRefs.current.delete(viewID);
    },
    [],
  );
  const recalculateVisibleViews = useCallback(() => {
    const strip = stripRef.current;
    const moreButton = moreButtonRef.current;
    if (!strip || !moreButton || strip.clientWidth <= 0) return;

    const widths = new Map<string, number>();
    for (const view of orderedViews) {
      const width = tabRefs.current.get(view.id)?.offsetWidth ?? 0;
      if (width <= 0) return;
      widths.set(view.id, width);
    }

    const actionWidth = activeActionsRef.current?.offsetWidth ?? 0;
    const reservedWidth =
      moreButton.offsetWidth +
      (actionWidth > 0 ? actionWidth + VIEW_STRIP_GAP : 0);
    const next = visibleSavedViewIDs(
      orderedViews,
      activeView?.id ?? null,
      widths,
      Math.max(0, strip.clientWidth - reservedWidth - VIEW_STRIP_GAP),
    );
    setVisibleViewIDs((current) =>
      current !== null &&
      current.length === next.length &&
      current.every((viewID, index) => viewID === next[index])
        ? current
        : next,
    );
  }, [activeView?.id, orderedViews]);

  useLayoutEffect(() => {
    recalculateVisibleViews();
    const strip = stripRef.current;
    if (!strip) return;
    const observer = new ResizeObserver(recalculateVisibleViews);
    observer.observe(strip);
    return () => observer.disconnect();
  }, [dirty, recalculateVisibleViews]);

  const effectiveVisibleViewIDs =
    visibleViewIDs ?? orderedViews.map((view) => view.id);
  const visibleViewIDSet = new Set(effectiveVisibleViewIDs);
  const overflowViews = orderedViews.filter(
    (view) => !visibleViewIDSet.has(view.id),
  );
  const privateOverflowViews = overflowViews.filter(
    (view) => view.visibility === "private",
  );
  const workspaceOverflowViews = overflowViews.filter(
    (view) => view.visibility === "workspace",
  );

  const announceDiscardedWorkingCopy = useCallback(() => {
    if (!activeView || !dirty) return;
    const discardedViewID = activeView.id;
    const discardedDefinition = currentDefinition;
    toast(t(($) => $.saved_views.changes_discarded), {
      action: {
        label: t(($) => $.saved_views.undo),
        onClick: () => {
          pendingDirtyRestoreRef.current = {
            viewID: discardedViewID,
            definition: discardedDefinition,
          };
          suppressDefaultRef.current = true;
          navigateToView(discardedViewID);
        },
      },
    });
  }, [activeView, currentDefinition, dirty, navigateToView, t]);

  const selectSavedView = useCallback(
    (viewID: string) => {
      if (viewID === activeView?.id) return;
      announceDiscardedWorkingCopy();
      suppressDefaultRef.current = true;
      navigateToView(viewID);
    },
    [activeView?.id, announceDiscardedWorkingCopy, navigateToView],
  );

  const selectBuiltInView = useCallback(() => {
    announceDiscardedWorkingCopy();
    restoreDefault();
  }, [announceDiscardedWorkingCopy, restoreDefault]);

  const openSaveAs = (name = "") => {
    setSaveAsName(name);
    setSaveDialogOpen(true);
  };

  const saveNewView = (
    name: string,
    visibility: IssueViewVisibility,
    template: IssueViewTemplateId,
  ) => {
    if (!apiScope) return;
    const definition = applyIssueViewTemplate(currentDefinition, template);
    createView.mutate(
      { ...apiScope, name, visibility, definition },
      {
        onSuccess: (view) => {
          setSaveDialogOpen(false);
          navigateToView(view.id);
          toast.success(t(($) => $.saved_views.created));
        },
        onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.saved_views.failed)),
      },
    );
  };

  const saveActiveView = () => {
    if (!activeView) return;
    updateView.mutate(
      { id: activeView.id, data: { definition: currentDefinition } },
      {
        onSuccess: (view) => {
          appliedViewRef.current = `${view.id}:${view.updated_at}`;
          appliedDefinitionRef.current = view.definition;
          toast.success(t(($) => $.saved_views.saved));
        },
        onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.saved_views.failed)),
      },
    );
  };

  const resetActiveView = () => {
    if (!activeView) return;
    onContextChange?.(contextFromDefinition(activeView.definition, scope));
    applyIssueSurfaceSavedView(surfaceKey, store, activeView.definition);
  };

  const renameActiveView = (name: string) => {
    if (!renameView) return;
    updateView.mutate(
      { id: renameView.id, data: { name } },
      {
        onSuccess: () => {
          setRenameView(null);
          toast.success(t(($) => $.saved_views.renamed));
        },
        onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.saved_views.failed)),
      },
    );
  };

  const duplicateActiveView = () => {
    if (!activeView) return;
    duplicateView.mutate(
      {
        id: activeView.id,
        data: {
          name: t(($) => $.saved_views.copy_name, { name: activeView.name }),
          visibility: "private",
        },
      },
      {
        onSuccess: (view) => {
          navigateToView(view.id);
          toast.success(t(($) => $.saved_views.duplicated));
        },
        onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.saved_views.failed)),
      },
    );
  };

  const deleteActiveView = () => {
    if (!activeView) return;
    deleteView.mutate(activeView.id, {
      onSuccess: () => {
        setDeleteDialogOpen(false);
        restoreDefault();
        toast.success(t(($) => $.saved_views.deleted));
      },
      onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.saved_views.failed)),
    });
  };

  const savedViewsAvailable =
    !!apiScope &&
    !(query.error instanceof ApiError && query.error.status === 404);
  const isSavedViewActive = savedViewsAvailable && !!urlViewId;

  const savedViewsControl = savedViewsAvailable ? (
    <div
      ref={stripRef}
      className="flex min-w-0 flex-1 items-center gap-1"
      data-testid="saved-view-strip"
    >
      <div className="relative flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
        {orderedViews.map((view) => {
          const visible = visibleViewIDSet.has(view.id);
          const selected = view.id === activeView?.id;
          return (
            <Button
              key={view.id}
              ref={(node) => setViewTabRef(view.id, node)}
              variant="outline"
              size="sm"
              aria-pressed={selected}
              aria-hidden={!visible}
              aria-label={
                selected && dirty
                  ? `${view.name} — ${t(($) => $.saved_views.unsaved_changes)}`
                  : view.name
              }
              tabIndex={visible ? 0 : -1}
              title={view.name}
              className={
                visible
                  ? selected
                    ? "max-w-44 gap-1.5 bg-accent text-accent-foreground hover:bg-accent/80"
                    : "max-w-44 gap-1.5 text-muted-foreground"
                  : "pointer-events-none invisible absolute left-0 top-0 max-w-44 gap-1.5"
              }
              onClick={() => selectSavedView(view.id)}
            >
              <span className="truncate">{view.name}</span>
              {selected && dirty && (
                <span
                  className="size-1.5 shrink-0 rounded-full bg-amber-500"
                  aria-hidden="true"
                />
              )}
            </Button>
          );
        })}

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                ref={moreButtonRef}
                variant="outline"
                size="sm"
                className="shrink-0 gap-1 text-muted-foreground"
                aria-label={t(($) => $.saved_views.more_views)}
              />
            }
          >
            {t(($) => $.saved_views.more_views)}
            <ChevronDown className="size-3 text-muted-foreground" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-64">
            {privateOverflowViews.length > 0 && (
              <DropdownMenuGroup>
                <DropdownMenuLabel>
                  {t(($) => $.saved_views.private_views)}
                </DropdownMenuLabel>
                {privateOverflowViews.map((view) => (
                  <DropdownMenuItem
                    key={view.id}
                    onClick={() => selectSavedView(view.id)}
                  >
                    <LockKeyhole className="text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate">{view.name}</span>
                    {query.data?.default_view_id === view.id && (
                      <Star className="size-3 fill-current text-amber-500" />
                    )}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            )}
            {workspaceOverflowViews.length > 0 && (
              <DropdownMenuGroup>
                <DropdownMenuLabel>
                  {t(($) => $.saved_views.workspace_views)}
                </DropdownMenuLabel>
                {workspaceOverflowViews.map((view) => (
                  <DropdownMenuItem
                    key={view.id}
                    onClick={() => selectSavedView(view.id)}
                  >
                    <Users className="text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate">{view.name}</span>
                    {query.data?.default_view_id === view.id && (
                      <Star className="size-3 fill-current text-amber-500" />
                    )}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            )}
            {orderedViews.length === 0 && (
              <DropdownMenuItem disabled>
                <span className="text-muted-foreground">
                  {t(($) => $.saved_views.no_custom_views)}
                </span>
              </DropdownMenuItem>
            )}
            {(overflowViews.length > 0 || orderedViews.length === 0) && (
              <DropdownMenuSeparator />
            )}
            <DropdownMenuItem
              onClick={() =>
                openSaveAs(
                  activeView
                    ? t(($) => $.saved_views.copy_name, { name: activeView.name })
                    : "",
                )
              }
            >
              <BookmarkPlus />
              {activeView
                ? t(($) => $.saved_views.save_as)
                : t(($) => $.saved_views.save_view)}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <div ref={activeActionsRef} className="flex shrink-0 items-center gap-1">
        {activeView && dirty && (
          <>
            <Button
              size="xs"
              variant="ghost"
              className="shrink-0 text-muted-foreground"
              onClick={resetActiveView}
            >
              <RotateCcw className="size-3" />
              {t(($) => $.saved_views.reset)}
            </Button>
            {activeView.can_edit ? (
              <Button
                size="xs"
                className="shrink-0"
                disabled={updateView.isPending}
                onClick={saveActiveView}
              >
                <Save className="size-3" />
                {t(($) => $.saved_views.save)}
              </Button>
            ) : (
              <Button
                size="xs"
                className="shrink-0"
                onClick={() =>
                  openSaveAs(
                    t(($) => $.saved_views.copy_name, {
                      name: activeView.name,
                    }),
                  )
                }
              >
                <BookmarkPlus className="size-3" />
                {t(($) => $.saved_views.save_as)}
              </Button>
            )}
          </>
        )}

        {activeView && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  size="icon-xs"
                  variant="ghost"
                  aria-label={t(($) => $.saved_views.more)}
                />
              }
            >
              <Ellipsis />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52">
              {activeView.can_edit && (
                <DropdownMenuItem onClick={() => setRenameView(activeView)}>
                  <Pencil />
                  {t(($) => $.saved_views.rename)}
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onClick={duplicateActiveView}>
                <CopyPlus />
                {t(($) => $.saved_views.duplicate)}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  void copyText(
                    navigation.getShareableUrl(
                      pathWithView(
                        navigation.pathname,
                        navigation.searchParams,
                        activeView.id,
                      ),
                    ),
                  ).then((ok) => {
                    if (ok) toast.success(t(($) => $.saved_views.link_copied));
                  });
                }}
              >
                <Copy />
                {t(($) => $.saved_views.copy_link)}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={() => {
                  if (!apiScope) return;
                  setDefault.mutate(
                    defaultIssueViewRequest(
                      apiScope,
                      isDefault ? null : activeView.id,
                    ),
                    {
                      onSuccess: () =>
                        toast.success(
                          isDefault
                            ? t(($) => $.saved_views.default_cleared)
                            : t(($) => $.saved_views.default_set),
                        ),
                    },
                  );
                }}
              >
                {isDefault ? <StarOff /> : <Star />}
                {isDefault
                  ? t(($) => $.saved_views.clear_default)
                  : t(($) => $.saved_views.set_default)}
                {isDefault && <Check className="ml-auto" />}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  if (isPinned) {
                    deletePin.mutate({ itemType: "view", itemId: activeView.id });
                  } else {
                    createPin.mutate({
                      item_type: "view",
                      item_id: activeView.id,
                    });
                  }
                }}
              >
                {isPinned ? <PinOff /> : <Pin />}
                {isPinned
                  ? t(($) => $.saved_views.unpin)
                  : t(($) => $.saved_views.pin)}
              </DropdownMenuItem>
              {activeView.can_edit && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setDeleteDialogOpen(true)}
                  >
                    <Trash2 />
                    {t(($) => $.saved_views.delete)}
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
    </div>
  ) : null;

  return (
    <>
      {children?.({
        savedViewsControl,
        isSavedViewActive,
        selectBuiltInView,
      })}

      <SaveViewDialog
        open={saveDialogOpen}
        defaultName={saveAsName}
        isMyIssues={scope.type === "my"}
        pending={mutationPending}
        onOpenChange={setSaveDialogOpen}
        onSave={saveNewView}
      />
      <RenameViewDialog
        view={renameView}
        pending={updateView.isPending}
        onClose={() => setRenameView(null)}
        onRename={renameActiveView}
      />
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.saved_views.delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.saved_views.delete_description, { name: activeView?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.saved_views.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={deleteView.isPending}
              onClick={deleteActiveView}
            >
              {t(($) => $.saved_views.delete)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
