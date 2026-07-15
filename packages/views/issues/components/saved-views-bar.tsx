"use client";

import {
  useCallback,
  useEffect,
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
  const onContextChangeRef = useRef(onContextChange);
  const suppressDefaultRef = useRef(false);

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
    <div className="flex shrink-0 items-center gap-1">
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="outline"
              size="sm"
              className={
                isSavedViewActive
                  ? "max-w-52 shrink-0 gap-1.5 bg-accent text-accent-foreground hover:bg-accent/80"
                  : "max-w-52 shrink-0 gap-1.5 text-muted-foreground"
              }
            />
          }
        >
          {activeView?.visibility === "private" ? (
            <LockKeyhole className="size-3.5 text-muted-foreground" />
          ) : activeView ? (
            <Users className="size-3.5 text-muted-foreground" />
          ) : (
            <LayoutTemplate className="size-3.5 text-muted-foreground" />
          )}
          <span className="truncate">
            {activeView?.name ?? t(($) => $.saved_views.custom_views)}
          </span>
          {dirty && <span className="size-1.5 rounded-full bg-amber-500" />}
          <ChevronDown className="size-3 text-muted-foreground" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              {t(($) => $.saved_views.custom_views)}
            </DropdownMenuLabel>
            {views.length === 0 ? (
              <DropdownMenuItem disabled>
                <span className="text-muted-foreground">
                  {t(($) => $.saved_views.no_custom_views)}
                </span>
              </DropdownMenuItem>
            ) : (
              views.map((view) => {
                const selected = view.id === activeView?.id;
                return (
                  <DropdownMenuItem
                    key={view.id}
                    onClick={() => {
                      suppressDefaultRef.current = true;
                      navigateToView(view.id);
                    }}
                  >
                    {view.visibility === "private" ? (
                      <LockKeyhole className="text-muted-foreground" />
                    ) : (
                      <Users className="text-muted-foreground" />
                    )}
                    <span className="min-w-0 flex-1 truncate">{view.name}</span>
                    {query.data?.default_view_id === view.id && (
                      <Star className="size-3 fill-current text-amber-500" />
                    )}
                    {selected && <Check className="size-3.5" />}
                  </DropdownMenuItem>
                );
              })
            )}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
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
          {activeView.can_edit && (
            <Button
              size="xs"
              className="shrink-0"
              disabled={updateView.isPending}
              onClick={saveActiveView}
            >
              <Save className="size-3" />
              {t(($) => $.saved_views.save)}
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
  ) : null;

  return (
    <>
      {children?.({
        savedViewsControl,
        isSavedViewActive,
        selectBuiltInView: restoreDefault,
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
