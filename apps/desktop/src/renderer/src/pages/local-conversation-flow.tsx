import { observer } from "mobx-react-lite";
import { conversationService } from "../platform/conversation-services";
import { localV2Error, LocalV2RequestError } from "@multica/conversation-client/services";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { LocalRuntimeNotice } from "./local-runtime-notice";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Archive, ChevronDown, ChevronRight, CircleHelp, Clock3, Copy, FolderKanban, FolderMinus, FolderOpen, FolderPlus, GitFork, Loader2, MonitorSmartphone, MoreHorizontal, Pencil, Pin, PinOff, Plus, Search, Square } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Input } from "@multica/ui/components/ui/input";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubTrigger, DropdownMenuSubContent, DropdownMenuRadioGroup, DropdownMenuRadioItem } from "@multica/ui/components/ui/dropdown-menu";
import { ContextMenu, ContextMenuTrigger, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuSub, ContextMenuSubTrigger, ContextMenuSubContent, ContextMenuRadioGroup, ContextMenuRadioItem } from "@multica/ui/components/ui/context-menu";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@multica/ui/components/ui/dialog";
import { copyText } from "@multica/ui/lib/clipboard";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useRequiredWorkspaceSlug, useWorkspacePaths } from "@multica/core/paths";
import { getCurrentSlug } from "@multica/core/platform";
import { projectListOptions } from "@multica/core/projects/queries";
import { useNavigation, AppLink } from "@multica/views/navigation";
import { useLocale, useT } from "@multica/views/i18n";
import { ProviderLogo } from "@multica/views/runtimes/provider-logo";
import { TuttiAgentGuiPanel } from "../components/tutti-agent-gui-panel";
import type { ParentServiceOptions } from "../platform/agent-gui-parent-service";
import type { LocalFolderProject, LocalProviderId } from "@multica/core/local-conversation/protocol";
import { isAutomaticLocalDirectory } from "@multica/tutti-agent-gui-host/local-projects";
import { createLocalConversationParentService, localConversationKey, localFolderProjectsQuery, localProvidersQuery, localTargetId, type LocalFolderProjectList, type LocalProviderList } from "../platform/local-conversation-service";
import { useLocalConversationPages } from "../platform/local-conversation-pages";
import { projectForDirectory, sessionEntry, type LocalSessionEntry } from "../platform/local-conversation-projects";
import { LocalConversationImportButton } from "./local-conversation-import";
import { LocalSideWorkspace, type SideWorkspaceHandle } from "./local-side-workspace";
import { PanelRightOpen } from "lucide-react";
import type { ComponentProps } from "react";
type RestoreDraftRequest = NonNullable<ComponentProps<typeof TuttiAgentGuiPanel>["restoreDraftRequest"]>;
import { projectVisibilityScopeKey, useProjectVisibilityStore } from "../stores/local-conversation-project-visibility";
import { SESSION_LIST_MIN_WIDTH, SESSION_LIST_MAX_WIDTH, useConversationLayoutStore } from "../stores/local-conversation-layout";
import { localV2Transport, setRemoteFolderChooser } from "../platform/local-v2-transport";
import { LocalDeviceProvider, useLocalDevice, type LocalDevice } from "../platform/local-device";
import { deviceName, useLinkedDevices } from "../platform/linked-devices";
import { pickRemoteFolder, RemoteFolderPicker } from "./remote-folder-picker";
import { useDocumentTitle } from "../hooks/use-document-title";
import { useConversationFocus } from "../platform/use-conversation-focus";

const EMPTY_PROJECT_IDS: string[] = [];
function folderProject(project: { id: string; title: string; path: string; created_at: string }): LocalFolderProject {
  return { id: project.id, title: project.title, path: project.path, createdAt: project.created_at };
}

/** A daemon without the local v2 API answers its routes with 404. */
function localProjectsRequireRestart(error: unknown): boolean {
  return error instanceof LocalV2RequestError && error.kind === "http_404";
}

const DEVICE_ROUTE = /\/conversations\/devices\/([^/?#]+)/;

// A folder the user picked on another device's page, applied by main.
setRemoteFolderChooser(async (deviceId, scope, request) => {
  const path = await pickRemoteFolder(deviceId, scope, request);
  return path === null ? null : window.desktopAPI.localV2.chooseRemote(deviceId, scope, request, path);
});

/**
 * Conversations of this device, or of another device of the account over
 * Device link (`/conversations/devices/{peer}`). The page is the same; only
 * where data comes from and where agents run changes.
 */
export function LocalConversationFlowPage() {
  const { pathname } = useNavigation();
  const match = DEVICE_ROUTE.exec(pathname);
  const deviceId = match ? decodeURIComponent(match[1]!) : "local";
  const devices = useLinkedDevices(deviceId !== "local");
  const name = deviceId === "local" ? "" : deviceName(devices, deviceId);
  const device = useMemo<LocalDevice>(() => ({ id: deviceId, name, transport: localV2Transport(deviceId) }), [deviceId, name]);
  return <LocalDeviceProvider device={device}><LocalConversationFlow key={deviceId} /></LocalDeviceProvider>;
}

const LocalConversationFlow = observer(function LocalConversationFlow() {
  const workspace = useCurrentWorkspace();
  const slug = useRequiredWorkspaceSlug();
  const userId = useAuthStore((state) => state.user?.id);
  const paths = useWorkspacePaths();
  const { pathname, searchParams, replace, openInNewTab, push } = useNavigation();
  const chinese = useLocale().startsWith("zh");
  const { t } = useT("chat");
  const compact = useIsCompact();
  const [sessionListWidth] = useState(() => useConversationLayoutStore.getState().sessionListWidth);
  const sessionListPanel = useRef<HTMLDivElement>(null);
  const resizeCleanup = useRef<(() => void) | null>(null);
  useEffect(() => () => resizeCleanup.current?.(), []);
  // Responsive constraints may shrink the panel without changing the user's preference.
  const rememberSessionResize = (endEvent: "pointerup" | "keyup") => {
    if (compact || resizeCleanup.current) return;
    const before = sessionListPanel.current?.getBoundingClientRect().width;
    const finish = () => {
      resizeCleanup.current?.();
      const width = sessionListPanel.current?.getBoundingClientRect().width;
      if (width !== undefined && width !== before) useConversationLayoutStore.getState().setSessionListWidth(width);
    };
    resizeCleanup.current = () => {
      window.removeEventListener(endEvent, finish);
      window.removeEventListener("pointercancel", finish);
      window.removeEventListener("blur", finish);
      resizeCleanup.current = null;
    };
    window.addEventListener(endEvent, finish);
    window.addEventListener("pointercancel", finish);
    window.addEventListener("blur", finish);
  };
  const qc = useQueryClient();
  const device = useLocalDevice();
  const { transport } = device;
  const remote = device.id !== "local";
  const flowPath = remote ? paths.conversationFlowDevice(device.id) : paths.conversationFlow();
  const { t: tLayout } = useT("layout");
  useDocumentTitle(remote ? tLayout(($) => $.nav.conversation_flow_device, { device: device.name }) : tLayout(($) => $.nav.conversation_flow));
  const sideWorkspace = useRef<SideWorkspaceHandle>(null);
  const [sideOpen, setSideOpen] = useState(false);
  const [referenceDraft, setReferenceDraft] = useState<RestoreDraftRequest | null>(null);
  const [locateRequest, setLocateRequest] = useState<{ sessionId: string; turnId?: string; quote?: string; sequence: number } | null>(null);
  const scope = { userId: userId || "", workspaceId: workspace?.id || "" };
  const v2 = conversationService(transport, scope);
  const visibilityScope = projectVisibilityScopeKey(scope);
  const hiddenProjectPreferences = useProjectVisibilityStore((state) => state.hiddenByScope[visibilityScope] ?? EMPTY_PROJECT_IDS);
  const setProjectHidden = useProjectVisibilityStore((state) => state.setProjectHidden);
  const [search, setSearch] = useState("");
  const [archived, setArchived] = useState(false);
  const [showList, setShowList] = useState(false);
  const [newConversation, setNewConversation] = useState<{ scope: string; request: NonNullable<ComponentProps<typeof TuttiAgentGuiPanel>["newConversationRequest"]> }>();
  const newConversationRequest = newConversation?.scope === visibilityScope ? newConversation.request : undefined;
  const focusIntentSequence = useRef(0);
  const [addingProject, setAddingProject] = useState(false);
  const [forkingId, setForkingId] = useState<string | null>(null);
  const forking = forkingId !== null;
  const [updatingId, setUpdatingId] = useState<string | null>(null);
  const [renameTarget, setRenameTarget] = useState<Pick<LocalSessionEntry, "id" | "title"> | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [renameError, setRenameError] = useState<string | null>(null);
  const renameInput = useRef<HTMLInputElement>(null);
  const renameReturnFocus = useRef<HTMLAnchorElement | null>(null);
  const sessionLinks = useRef(new Map<string, HTMLAnchorElement>());
  const renameErrorId = useId();
  const [contextSelection, setContextSelection] = useState("");
  const [collapsedProjects, setCollapsedProjects] = useState<Set<string>>(() => new Set());
  const [focusSequence, setFocusSequence] = useState(0);
  const providers = useQuery({ ...localProvidersQuery(v2, device.id), select: (value: LocalProviderList) => value.providers, enabled: !!workspace && !!userId });
  const projectQuery = useQuery({ ...projectListOptions(scope.workspaceId), enabled: !!workspace && !!userId });
  const projects = (projectQuery.data ?? []).filter((project) => project.workspace_id === scope.workspaceId);
  const folderQuery = useQuery({ ...localFolderProjectsQuery(v2, device.id), select: (value: LocalFolderProjectList) => value.projects.map(folderProject), enabled: !!workspace && !!userId,
    retry: (count, error) => !localProjectsRequireRestart(error) && count < 2 });
  useEffect(() => {
    if (!scope.userId || !scope.workspaceId) return;
    const key = localConversationKey({ userId: scope.userId, workspaceId: scope.workspaceId }, device.id);
    const refresh = () => {
      void qc.invalidateQueries({ queryKey: [...key, "providers"] });
      void qc.invalidateQueries({ queryKey: [...key, "folder-projects"] });
      void qc.invalidateQueries({ queryKey: [...key, "selected"] });
    };
    let connected = transport.availability()?.state === "connected";
    const stopAvailability = transport.onAvailability((value) => {
      const next = value.state === "connected";
      if (next && !connected) refresh();
      connected = next;
    });
    const stopConnection = transport.onConnection((value, online) => {
      if (online && value.userId === scope.userId && value.workspaceId === scope.workspaceId) refresh();
    });
    return () => { stopAvailability(); stopConnection(); };
  }, [transport, qc, device.id, scope.userId, scope.workspaceId]);
  const folderProjects = folderQuery.data ?? [];
  const hiddenProjectIds = hiddenProjectPreferences.filter((id) => folderProjects.some((project) => project.id === id));
  const selectedId = searchParams.get("session");
  const sessions = useLocalConversationPages(transport, scope, folderProjects, hiddenProjectIds, archived, search, !!workspace && !!userId);
  // The list already knows every conversation; only an ID it lacks needs its snapshot read here.
  const selectedSummary = selectedId ? v2.list.summary(selectedId) : undefined;
  const selectedQuery = useQuery({ queryKey: [...localConversationKey(scope, device.id), "selected", selectedId], queryFn: async () => sessionEntry((await v2.snapshot(selectedId!)).conversation), enabled: !!workspace && !!userId && !!selectedId && !selectedSummary && !sessions.isPending, retry: false, refetchOnMount: "always" });
  const selected = selectedSummary ? sessionEntry(selectedSummary) : selectedQuery.data;
  const selectedRef = useRef(selectedId); selectedRef.current = selectedId;
  const provider = selected?.provider ?? (searchParams.get("provider") === "claude" ? "claude" : "codex");
  const active = pathname === flowPath && getCurrentSlug() === slug;
  useConversationFocus(v2, selectedId, active);
  const intent = `${userId}:${workspace?.id}:${selectedId}:${provider}`;
  const currentIntent = useRef(intent);
  currentIntent.current = intent;
  const latest = useRef({ select: (_id: string | null, _provider?: LocalProviderId) => {}, locate: (_id: string, _turnId?: string, _quote?: string) => {} });
  const select = (id: string | null, choice: LocalProviderId = provider) => {
    const query = new URLSearchParams({ provider: choice }); if (id) query.set("session", id);
    replace(`${flowPath}?${query}`); setShowList(false);
  };
  latest.current.select = select;
  latest.current.locate = (id, turnId, quote) => {
    const source = sessions.data?.find((entry) => entry.id === id) ?? (selected?.id === id ? selected : undefined);
    const sequence = ++focusIntentSequence.current;
    const startedIntent = currentIntent.current;
    const locate = (entry: LocalSessionEntry) => {
      if (sequence !== focusIntentSequence.current || currentIntent.current !== startedIntent) return;
      setSearch(""); setArchived(entry.archived);
      const project = projectForDirectory(folderProjects, entry.cwd);
      setCollapsedProjects((previous) => { const next = new Set(previous); next.delete(project?.id ?? "unassigned"); return next; });
      select(id, entry.provider); setLocateRequest({ sessionId: id, turnId, quote, sequence });
    };
    if (source) locate(source);
    else void qc.fetchQuery({ queryKey: [...localConversationKey(scope, device.id), "selected", id], queryFn: async () => sessionEntry((await v2.snapshot(id)).conversation) }).then(locate).catch((error: unknown) => toast.error(error instanceof Error ? error.message : String(error)));
  };
  const factory = useCallback((options: ParentServiceOptions) => createLocalConversationParentService({ ...options, locale: chinese ? "zh-CN" : "en", getSelectionIntent: () => currentIntent.current, getSelectedSessionId: () => selectedRef.current, onLocalSelect: (id, choice) => latest.current.select(id, choice), onOpenSide: (id, sourceId) => sideWorkspace.current?.openSide(id, sourceId), onOpenResource: (id, url) => { void sideWorkspace.current?.openResource(id, url, id).catch((error: unknown) => toast.error(error instanceof Error ? error.message : String(error))); }, onLocateSource: (sessionId, turnId, quote) => latest.current.locate(sessionId, turnId, quote) }, transport), [transport, chinese]);
  useEffect(() => {
    // Restored tabs may still contain an old cloud chat ID. Cloud history remains in Chat.
    // A stale list or the outgoing page must not clear another tab's selection.
    if (active && selectedQuery.isError && selectedQuery.error instanceof LocalV2RequestError && selectedQuery.error.kind === "not_found" && selectedId) latest.current.select(null, provider);
  }, [active, selectedQuery.isError, selectedQuery.error, selectedId, provider]);
  if (!workspace || !userId) return null;
  const labels = chinese ? {
    sessions: "本机会话", new: "新建会话", search: "搜索会话", empty: "还没有本机会话", noResults: "没有匹配的会话", retry: "重试", loading: "正在加载…", archive: "归档", history: "返回会话", pinned: "置顶", unpin: "取消置顶", directory: "选择工作目录", newTitle: "新建会话", storage: "历史保存在本机", directoryLocked: "工作目录在首次发送后固定；如需更换，请新建会话。", unavailable: "请先在本机安装 Codex 或 Claude Code", failed: "操作失败，请重试", fork: "分叉", forking: "正在分叉…", forkHint: "从当前历史创建独立会话", forkWaiting: "执行结束后可分叉", forkEmpty: "发送消息后可分叉",
  } : {
    sessions: "Local sessions", new: "New conversation", search: "Search conversations", empty: "No local conversations yet", noResults: "No matching conversations", retry: "Retry", loading: "Loading…", archive: "Archive", history: "Back to sessions", pinned: "Pin", unpin: "Unpin", directory: "Choose working directory", newTitle: "New conversation", storage: "History saved on this computer", directoryLocked: "The working directory is fixed after the first message. Start a new conversation to change it.", unavailable: "Install Codex or Claude Code on this computer", failed: "Could not update the conversation", fork: "Fork", forking: "Forking…", forkHint: "Create an independent conversation from this history", forkWaiting: "Wait for the current execution to finish", forkEmpty: "Send a message before forking",
  };
  const report = (error: unknown) => toast.error(error instanceof Error ? error.message : labels.failed);
  const canCreate = providers.data?.some((entry) => entry.available) ?? false;
  const providerLabels = chinese ? { checking: "正在检测本机运行器…", failed: "运行器检测失败" }
    : { checking: "Checking local providers…", failed: "Could not detect providers" };
  const createDisabledReason = providers.isPending ? providerLabels.checking : providers.isError ? providerLabels.failed : labels.unavailable;
  const renameLabels = chinese ? { action: "重命名", title: "重命名会话", name: "会话名称", save: "保存", saving: "正在保存...", cancel: "取消" }
    : { action: "Rename", title: "Rename conversation", name: "Conversation name", save: "Save", saving: "Saving...", cancel: "Cancel" };
  const projectLabels = chinese ? { unassigned: "未分配项目", unavailable: "项目不可用", actions: "会话操作", move: "关联工作区项目", noWorkspaceProject: "不关联工作区项目", create: "新建项目会话", new: "新建项目", empty: "暂无会话", loadFailed: "项目加载失败", folderLoadFailed: "本地项目加载失败", manage: "项目操作", remove: "从列表移除", removed: "已移除项目", restore: "恢复项目", removedNotice: "已从列表移除", undo: "撤销" }
    : { unassigned: "No project", unavailable: "Unavailable project", actions: "Conversation actions", move: "Link workspace project", noWorkspaceProject: "No workspace project", create: "New conversation in project", new: "New project", empty: "No conversations", loadFailed: "Could not load projects", folderLoadFailed: "Could not load local projects", manage: "Project actions", remove: "Remove from list", removed: "Removed projects", restore: "Restore project", removedNotice: "Project removed from list", undo: "Undo" };
  const folderRestartMessage = chinese ? "本地项目功能已更新，请重启桌面应用" : "Local projects have been updated. Restart the desktop app.";
  const groups = sessions.groups;
  const addProject = async () => {
    if (addingProject) return;
    setAddingProject(true);
    const startedIntent = intent;
    try {
      const added = await transport.choose(scope, { purpose: "folder_project", ...(folderProjects.at(-1) ? { defaultPath: folderProjects.at(-1)!.path } : {}) }) as { id: string; title: string; path: string; created_at: string } | null;
      if (!added) return;
      const project = folderProject(added);
      await qc.invalidateQueries({ queryKey: localConversationKey(scope, device.id) });
      if (currentIntent.current === startedIntent) {
        setSearch(""); setArchived(false); setProjectHidden(scope, project.id, false);
        setCollapsedProjects((previous) => { const next = new Set(previous); next.delete(project.id); return next; });
      }
    } catch (raw) { const error = localV2Error(raw); if (localProjectsRequireRestart(error)) toast.error(folderRestartMessage); else report(error); } finally { setAddingProject(false); }
  };
  const restoreProject = (projectId: string) => {
    setProjectHidden(scope, projectId, false);
    setSearch(""); setArchived(false);
    setCollapsedProjects((previous) => { const next = new Set(previous); next.delete(projectId); return next; });
  };
  const removeProject = (projectId: string) => {
    setProjectHidden(scope, projectId, true);
    toast.success(projectLabels.removedNotice, { action: { label: projectLabels.undo, onClick: () => setProjectHidden(scope, projectId, false) } });
  };
  const defaultFolderProjectId = projectForDirectory(folderProjects, selected?.cwd ?? newConversationRequest?.projectPath ?? "")?.id ?? null;
  const create = (folderProjectId: string | null = defaultFolderProjectId) => {
    const target = providers.data?.find((entry) => entry.id === provider && entry.available) ?? providers.data?.find((entry) => entry.available);
    if (!target) return;
    const sequence = ++focusIntentSequence.current;
    setNewConversation({ scope: visibilityScope, request: { sequence, projectPath: folderProjects.find((project) => project.id === folderProjectId)?.path ?? null } });
    setSearch(""); setArchived(false); setFocusSequence(sequence); latest.current.select(null, target.id);
  };
  const update = async (params: { id: string; title?: string; pinned?: boolean; archived?: boolean; projectId?: string | null }, onError: (error: unknown) => void = report) => {
    if (updatingId) return false;
    setUpdatingId(params.id);
    const startedIntent = intent;
    try {
      const { id, projectId, ...fields } = params;
      v2.receiveSummary(localV2SummarySchema.parse(await v2.command(id, { type: "update", ...fields, ...(projectId !== undefined ? { project_id: projectId } : {}) })));
      await qc.invalidateQueries({ queryKey: localConversationKey(scope, device.id) });
      if (params.archived !== undefined && selectedId === params.id && currentIntent.current === startedIntent) latest.current.select(null, provider);
      return true;
    } catch (error) { onError(error); return false; } finally { setUpdatingId(null); }
  };
  const startRename = (entry: LocalSessionEntry) => {
    renameReturnFocus.current = sessionLinks.current.get(entry.id) ?? null;
    setRenameDraft(entry.title); setRenameError(null); setRenameTarget({ id: entry.id, title: entry.title });
  };
  const stopBackground = async (entry: LocalSessionEntry) => {
    if (updatingId) return;
    setUpdatingId(entry.id);
    try { await v2.command(entry.id, { type: "stop_background_tasks" }); }
    catch (error) { report(error); } finally { setUpdatingId(null); }
  };
  const rename = async () => {
    const title = renameDraft.trim();
    if (!renameTarget || updatingId || !title || title.length > 256) return;
    if (title === renameTarget.title) { setRenameTarget(null); return; }
    setRenameError(null);
    if (await update({ id: renameTarget.id, title }, (error) => setRenameError(error instanceof Error ? error.message : labels.failed))) setRenameTarget(null);
  };
  const fork = async (entry = selected) => {
    if (forking || !entry || !entry.turnCount || entry.running) return;
    setForkingId(entry.id);
    const startedIntent = intent;
    try {
      const summary = localV2SummarySchema.parse(await v2.command(entry.id, { type: "fork", request_id: crypto.randomUUID() }));
      v2.receiveSummary(summary);
      const created = sessionEntry(summary);
      if (currentIntent.current === startedIntent) {
        setSearch(""); setArchived(false); setFocusSequence(++focusIntentSequence.current); latest.current.select(created.id, created.provider);
      }
    } catch (error) { report(error); } finally { setForkingId(null); }
  };
  // Internal execution folders, including those inherited by forks, are not user-bound folders.
  const automaticDirectory = isAutomaticLocalDirectory(selected?.cwd ?? "");
  const directoryName = automaticDirectory
    ? projectLabels.unassigned
    : selected?.cwd.split(/[\\/]/).filter(Boolean).at(-1) || labels.directory;
  const renderSessionActions = (entry: LocalSessionEntry, contextMenu: boolean) => {
    const Sub = contextMenu ? ContextMenuSub : DropdownMenuSub;
    const SubTrigger = contextMenu ? ContextMenuSubTrigger : DropdownMenuSubTrigger;
    const SubContent = contextMenu ? ContextMenuSubContent : DropdownMenuSubContent;
    const RadioGroup = contextMenu ? ContextMenuRadioGroup : DropdownMenuRadioGroup;
    const RadioItem = contextMenu ? ContextMenuRadioItem : DropdownMenuRadioItem;
    const Item = contextMenu ? ContextMenuItem : DropdownMenuItem;
    const Separator = contextMenu ? ContextMenuSeparator : DropdownMenuSeparator;
    const disabled = updatingId !== null;
    const entryRunning = entry.running;
    return <>
      <Item disabled={disabled} onClick={() => startRename(entry)}><Pencil />{renameLabels.action}</Item>
      <Item disabled={disabled || forking || entryRunning || !entry.turnCount} aria-busy={forkingId === entry.id}
        title={entryRunning ? labels.forkWaiting : !entry.turnCount ? labels.forkEmpty : labels.forkHint} onClick={() => { void fork(entry); }}>
        {forkingId === entry.id ? <Loader2 className="animate-spin" /> : <GitFork />}{forkingId === entry.id ? labels.forking : labels.fork}
      </Item>
      <Item disabled={disabled} onClick={() => { void update({ id: entry.id, pinned: !entry.pinned }); }}>{entry.pinned ? <PinOff /> : <Pin />}{entry.pinned ? labels.unpin : labels.pinned}</Item>
      {entry.backgroundTasks > 0 && <Item disabled={disabled} onClick={() => { void stopBackground(entry); }}><Square />{chinese ? "停止后台任务" : "Stop background tasks"}</Item>}
      <Separator />
      <Sub>
        <SubTrigger disabled={disabled}><FolderKanban />{projectLabels.move}</SubTrigger>
        <SubContent className="max-h-80 max-w-72 overflow-y-auto">
          <RadioGroup value={entry.projectId ?? "__none__"} onValueChange={(value) => { void update({ id: entry.id, projectId: value === "__none__" ? null : value }); }}>
            <RadioItem disabled={disabled} value="__none__">{projectLabels.noWorkspaceProject}</RadioItem>
            {projects.map((project) => <RadioItem key={project.id} disabled={disabled} value={project.id}><span className="truncate">{project.title}</span></RadioItem>)}
          </RadioGroup>
          {projectQuery.isPending && <Item disabled>{labels.loading}</Item>}
          {projectQuery.isError && <Item disabled={disabled} onClick={() => { void projectQuery.refetch(); }}>{projectLabels.loadFailed} · {labels.retry}</Item>}
        </SubContent>
      </Sub>
      <Item onClick={() => { void copyText(entry.id).then((copied) => {
        if (copied) toast.success(chinese ? "已复制会话 ID" : "Conversation ID copied");
        else toast.error(chinese ? "无法复制会话 ID，请重试" : "Could not copy conversation ID. Try again.");
      }); }}><Copy />{chinese ? "复制会话 ID" : "Copy conversation ID"}</Item>
      <Separator />
      <Item disabled={disabled} onClick={() => { void update({ id: entry.id, archived: !archived }); }}><Archive />{archived ? labels.history : labels.archive}</Item>
    </>;
  };
  const renderSession = (entry: LocalSessionEntry) => {
    const entryRunning = entry.running;
    const waitingForAnswer = entry.waitingForAnswer;
    const background = entry.backgroundTasks;
    return <ContextMenu key={entry.id}>
    <ContextMenuTrigger className="group relative block select-text" data-session-id={entry.id} onContextMenu={(event) => {
      const selection = window.getSelection();
      setContextSelection(selection && event.currentTarget.contains(selection.anchorNode) && event.currentTarget.contains(selection.focusNode) ? selection.toString() : "");
    }}>
    <AppLink ref={(node) => { if (node) sessionLinks.current.set(entry.id, node); else sessionLinks.current.delete(entry.id); }} href={`${flowPath}?${new URLSearchParams({ provider: entry.provider, session: entry.id })}`} aria-current={selectedId === entry.id ? "true" : undefined}
      onClick={(event) => { event.preventDefault(); select(entry.id, entry.provider); }}
      className={`flex min-w-0 items-start gap-3 rounded-lg px-3 py-3 text-body outline-none focus-visible:ring-2 focus-visible:ring-ring ${entryRunning || background ? "pr-16" : `group-hover:pr-11 group-focus-within:pr-11 ${updatingId === entry.id ? "pr-11" : ""}`} ${selectedId === entry.id ? "bg-accent text-accent-foreground" : "hover:bg-accent/60"}`}>
      <span aria-hidden="true" className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border bg-muted/50 text-foreground"><ProviderLogo provider={entry.provider} className="size-5" /></span>
      <span className="min-w-0 flex-1"><span className="flex items-center gap-1.5">{entry.pinned && <Pin aria-label={labels.pinned} className="size-3 shrink-0 text-muted-foreground" />}<span className="truncate font-medium">{entry.title}</span></span><span className={`mt-0.5 block truncate text-caption ${waitingForAnswer ? "text-foreground" : "text-muted-foreground"}`}>{waitingForAnswer ? t($ => $.desktop.local_conversation.waiting_for_answer) : <>{entry.provider === "codex" ? "Codex" : "Claude Code"} · {entry.preview || labels.storage}</>}</span></span>
    </AppLink>
    <div className="absolute right-2 top-1/2 flex -translate-y-1/2 items-center gap-1">
      <div className={`focus-within:opacity-100 group-hover:opacity-100 ${updatingId === entry.id ? "opacity-100" : "opacity-0"}`}>
        <DropdownMenu>
          <DropdownMenuTrigger render={<IconButton label={`${projectLabels.actions}：${entry.title}`} disabled={updatingId !== null} aria-busy={updatingId === entry.id} />}>{updatingId === entry.id ? <Loader2 className="size-3.5 animate-spin" /> : <MoreHorizontal className="size-3.5" />}</DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {renderSessionActions(entry, false)}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {waitingForAnswer ? <IconButton size="icon-xs" label={t($ => $.desktop.local_conversation.waiting_for_answer_label, { title: entry.title })} onClick={() => select(entry.id, entry.provider)}>
        <CircleHelp aria-hidden="true" className="size-4 text-muted-foreground" />
      </IconButton> : entryRunning && <Spinner aria-label={chinese ? `会话运行中：${entry.title}` : `Conversation running: ${entry.title}`} className="shrink-0 text-muted-foreground motion-reduce:animate-none" />}
      {!remote && (() => {
        const users = v2.list.presence(entry.id).map((user) => user.device_name || user.peer_id.slice(-6)).join(chinese ? "、" : ", ");
        return users && <span role="status" aria-label={chinese ? `${users} 正在使用：${entry.title}` : `In use on ${users}: ${entry.title}`} title={chinese ? `${users} 正在使用` : `In use on ${users}`} className="flex shrink-0 items-center text-muted-foreground"><MonitorSmartphone aria-hidden="true" className="size-4" /></span>;
      })()}
      {!entryRunning && background > 0 && <span role="status" aria-label={chinese ? `后台运行中：${entry.title}（${background}）` : `Background tasks: ${entry.title} (${background})`} title={chinese ? "后台运行中" : "Running in background"} className="flex shrink-0 items-center text-muted-foreground"><Clock3 aria-hidden="true" className="size-4" /></span>}
    </div>
    </ContextMenuTrigger>
    <ContextMenuContent aria-label={`${projectLabels.actions}：${entry.title}`}>
      {contextSelection.trim() && <>
        <ContextMenuItem onClick={() => { void copyText(contextSelection).then((copied) => { if (!copied) toast.error(chinese ? "无法复制文字，请重试" : "Could not copy text. Try again."); }); }}><Copy />{chinese ? "复制所选文字" : "Copy selected text"}</ContextMenuItem>
        <ContextMenuSeparator />
      </>}
      {renderSessionActions(entry, true)}
    </ContextMenuContent>
  </ContextMenu>;
  };
  return <div className="flex min-h-0 min-w-0 flex-1" data-testid="local-conversation-flow">
    <Dialog open={renameTarget !== null} onOpenChange={(open) => { if (!open && updatingId === null) setRenameTarget(null); }}>
      <DialogContent initialFocus={renameInput} finalFocus={renameReturnFocus} showCloseButton={updatingId === null}>
        <form className="grid gap-4" onSubmit={(event) => { event.preventDefault(); void rename(); }}>
          <DialogHeader><DialogTitle>{renameLabels.title}</DialogTitle></DialogHeader>
          <label className="grid gap-2 text-label">
            {renameLabels.name}
            <Input ref={renameInput} value={renameDraft} maxLength={256} required disabled={updatingId !== null} aria-invalid={Boolean(renameError)} aria-describedby={renameError ? renameErrorId : undefined}
              onFocus={(event) => event.currentTarget.select()} onChange={(event) => { setRenameDraft(event.target.value); setRenameError(null); }}
              onKeyDown={(event) => { if (event.key === "Enter" && (event.nativeEvent.isComposing || event.keyCode === 229)) event.preventDefault(); }} />
          </label>
          {renameError && <p id={renameErrorId} role="alert" className="text-caption text-destructive">{renameError}</p>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={updatingId !== null} onClick={() => setRenameTarget(null)}>{renameLabels.cancel}</Button>
            <Button type="submit" disabled={updatingId !== null || !renameDraft.trim()} aria-busy={updatingId !== null}>
              {updatingId !== null && <Loader2 className="animate-spin" />}{updatingId !== null ? renameLabels.saving : renameLabels.save}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
    <ResizablePanelGroup orientation="horizontal" disabled={compact}
      className={`relative min-h-0 min-w-0 flex-1 [&>[hidden]]:!hidden ${compact ? "[&>[data-panel]]:absolute [&>[data-panel]]:inset-0" : ""}`}>
      <ResizablePanel id="local-sessions" elementRef={sessionListPanel} defaultSize={sessionListWidth} minSize={SESSION_LIST_MIN_WIDTH} maxSize={SESSION_LIST_MAX_WIDTH} groupResizeBehavior="preserve-pixel-size" hidden={compact && !showList}>
        <div className="flex h-full min-h-0 min-w-0 flex-col border-r" data-testid="local-conversation-sessions">
          <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
            <h1 className="min-w-0 flex-1 truncate text-body font-semibold">{labels.sessions}</h1>
            <div className="flex shrink-0 items-center gap-1">
              <LocalConversationImportButton key={`${scope.userId}:${scope.workspaceId}`} scope={scope} client={transport} projects={projects} chinese={chinese} onImported={() => { void qc.invalidateQueries({ queryKey: localConversationKey(scope, device.id) }); }} />
              <IconButton label={projectLabels.new} disabled={addingProject} aria-busy={addingProject} onClick={() => { void addProject(); }}>{addingProject ? <Loader2 className="animate-spin" /> : <FolderPlus />}</IconButton>
              <IconButton label={labels.new} disabled={!canCreate} disabledReason={createDisabledReason} aria-busy={providers.isFetching || undefined} onClick={() => create()}><Plus /></IconButton>
            </div>
          </div>
          <div className="px-3 py-3"><div className="relative"><Search className="pointer-events-none absolute left-3 top-2.5 size-4 text-muted-foreground" /><Input aria-label={labels.search} placeholder={labels.search} className="pl-9" value={search} onChange={(event) => setSearch(event.target.value)} onKeyDown={(event) => { if (event.key === "Escape") setSearch(""); }} /></div></div>
          <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto px-3 pb-2">
            {providers.isError && <div role="status" aria-label={providerLabels.failed} className="flex items-center justify-between gap-2 px-3 py-1 text-caption text-muted-foreground"><span>{providerLabels.failed}</span><Button variant="ghost" size="xs" disabled={providers.isFetching} onClick={() => { void providers.refetch(); }}>{labels.retry}</Button></div>}
            {folderQuery.isError && <div role="status" className="flex items-center justify-between gap-2 px-3 py-1 text-caption text-muted-foreground"><span>{localProjectsRequireRestart(folderQuery.error) ? folderRestartMessage : projectLabels.folderLoadFailed}</span><Button variant="ghost" size="xs" onClick={() => { void folderQuery.refetch(); }}>{labels.retry}</Button></div>}
            {sessions.isPending ? <p role="status" className="p-4 text-body text-muted-foreground">{labels.loading}</p> : sessions.isError ? <div role="alert" className="p-4"><Button variant="outline" onClick={() => { void sessions.refetch(); }}>{labels.retry}</Button></div> : groups.length ? groups.map((group) => {
              const key = group.id ?? "unassigned";
              const expanded = Boolean(search.trim()) || !collapsedProjects.has(key);
              const title = group.id === null ? projectLabels.unassigned : group.project?.title ?? projectLabels.unavailable;
              return <section key={key} className="flex shrink-0 flex-col gap-2" aria-label={title}>
                <div className="flex items-center gap-1 rounded-md bg-muted pr-1">
                  <Button variant="ghost" size="sm" aria-label={title} title={group.project?.path} aria-expanded={expanded} className="min-w-0 flex-1 justify-start pr-0 text-label text-muted-foreground" onClick={() => setCollapsedProjects((previous) => { const next = new Set(previous); if (next.has(key)) next.delete(key); else next.add(key); return next; })}>
                    {expanded ? <ChevronDown className="size-3.5 shrink-0" /> : <ChevronRight className="size-3.5 shrink-0" />}<FolderOpen className="size-3.5 shrink-0" /><span className="truncate">{title}</span><span className="ml-auto text-micro tabular-nums text-faint-foreground">{group.sessions.length}</span>
                  </Button>
                  <div className="flex shrink-0 items-center">
                    {!archived && (group.id === null || group.project) && <IconButton size="icon-xs" label={`${projectLabels.create}：${title}`} disabled={!canCreate} disabledReason={createDisabledReason} onClick={() => create(group.id)}><Plus className="size-3.5" /></IconButton>}
                    {group.id !== null && <DropdownMenu>
                      <DropdownMenuTrigger
                        // Activate on click; desktop accessibility presses may omit compatibility mouse events.
                        onPointerDown={(event) => event.preventBaseUIHandler()}
                        onMouseDown={(event) => event.preventBaseUIHandler()}
                        render={<IconButton size="icon-xs" label={`${projectLabels.manage}：${title}`} />}
                      ><MoreHorizontal className="size-3.5" /></DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => removeProject(key)}><FolderMinus />{projectLabels.remove}</DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>}
                  </div>
                </div>
                {expanded && <>
                  {group.sessions.length ? <div className="flex flex-col gap-1">{group.sessions.map(renderSession)}</div> : <p className="px-3 py-2 text-caption text-muted-foreground">{projectLabels.empty}</p>}
                  {group.nextCursor && <Button variant="ghost" size="sm" className="mx-2 bg-muted/50 dark:bg-muted/25" disabled={sessions.loading.has(group.id) || sessions.isFetching} aria-busy={sessions.loading.has(group.id)} onClick={() => { void sessions.loadMore(group).catch(report); }}>{sessions.loading.has(group.id) ? labels.loading : chinese ? "加载更多" : "Load more"}</Button>}
                </>}
              </section>;
            }) : <p className="px-3 py-8 text-center text-body text-muted-foreground">{search ? labels.noResults : labels.empty}</p>}
          </div>
          {hiddenProjectIds.length > 0 && <div className="shrink-0 px-3 pt-1">
            <DropdownMenu>
              <DropdownMenuTrigger
                onPointerDown={(event) => event.preventBaseUIHandler()}
                onMouseDown={(event) => event.preventBaseUIHandler()}
                render={<Button variant="ghost" size="sm" className="text-caption text-muted-foreground" />}
              >
                {projectLabels.removed}{chinese ? `（${hiddenProjectIds.length}）` : ` (${hiddenProjectIds.length})`}<ChevronDown className="size-3.5" />
              </DropdownMenuTrigger>
              <DropdownMenuContent side="top" align="start" aria-label={projectLabels.restore} className="max-h-80 max-w-72 overflow-y-auto">
                {hiddenProjectIds.map((id) => {
                  const title = folderProjects.find((project) => project.id === id)?.title ?? `${projectLabels.unavailable} · ${id.slice(0, 8)}`;
                  return <DropdownMenuItem key={id} aria-label={`${projectLabels.restore}：${title}`} onClick={() => restoreProject(id)}><FolderPlus /><span className="truncate">{title}</span></DropdownMenuItem>;
                })}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>}
          <div className="flex items-center justify-between px-4 py-3 text-caption text-muted-foreground"><span className="font-normal">{labels.storage}</span><Button variant="ghost" size="sm" onClick={() => setArchived((value) => !value)}>{archived ? labels.history : labels.archive}</Button></div>
        </div>
      </ResizablePanel>
      <ResizableHandle hidden={compact} aria-label={chinese ? "调整会话列表宽度" : "Resize conversation list"}
        onPointerDown={(event) => { if (event.button === 0) rememberSessionResize("pointerup"); }}
        onKeyDown={(event) => { if (["ArrowLeft", "ArrowRight", "Home", "End", "Enter"].includes(event.key)) rememberSessionResize("keyup"); }} />
      <ResizablePanel id="local-detail" minSize="40%" hidden={compact && showList}>
        <LocalSideWorkspace key={`${workspace.id}:${userId}`} ref={sideWorkspace} onOpenChange={setSideOpen} sessionId={selectedId} scope={scope} slug={slug} source={selected} provider={provider} sessions={sessions.data ?? []} chinese={chinese} active={active && (!compact || !showList)} onSelectSource={(sessionId, turnId, quote) => latest.current.locate(sessionId, turnId, quote)} onReference={(content) => setReferenceDraft({ id: crypto.randomUUID(), content, sessionId: selectedId ?? undefined })} onOpenTask={(id) => { if (openInNewTab) openInNewTab(paths.issueDetail(id), undefined, { activate: true }); else push(paths.issueDetail(id)); }} onShowMessage={(message, tone) => toast[tone ?? "info"](message)}>
        {(mainVisible) =>
        <div className="flex h-full min-h-0 min-w-0 flex-col">
          <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
            {compact && <IconButton label={labels.sessions} onClick={() => setShowList(true)}><ArrowLeft /></IconButton>}
            <span className="min-w-0 flex-1 truncate text-body font-medium">{selected?.title ?? labels.newTitle}</span>
            <div className="flex min-w-0 items-center gap-1">
              {selected && <Button variant="ghost" size="sm" className="min-w-0 max-w-48 shrink" aria-label={labels.directory} disabled={selected.turnCount > 0} title={`${automaticDirectory ? directoryName : selected.cwd}${selected.turnCount ? ` · ${labels.directoryLocked}` : ""}`} onClick={() => { void transport.choose(scope, { purpose: "conversation_directory", conversationId: selected.id, defaultPath: selected.cwd }).then((changed) => { if (changed) v2.receiveSummary(localV2SummarySchema.parse(changed)); }).catch((error: unknown) => report(localV2Error(error))); }}><FolderOpen className="size-4" /><span className="truncate">{directoryName}</span></Button>}
              <IconButton aria-expanded={sideOpen} label={sideOpen ? (chinese ? "关闭右侧面板" : "Close right workspace") : (chinese ? "打开右侧面板" : "Open right workspace")} onClick={() => sideWorkspace.current?.toggle()}><PanelRightOpen /></IconButton>
            </div>
          </div>
          <LocalRuntimeNotice scope={scope} chinese={chinese} />
          {remote && <RemoteFolderPicker chinese={chinese} />}
          <TuttiAgentGuiPanel locateRequest={locateRequest ?? undefined} restoreDraftRequest={referenceDraft} key={`${workspace.id}:${userId}`} serviceFactory={factory} workspaceId={workspace.id} workspaceSlug={slug} userId={userId}
            activeSessionId={selectedId} selectedAgentId={localTargetId(provider)} active={active && mainVisible && (!compact || !showList)} composerFocusSequence={focusSequence || undefined}
            newConversationRequest={newConversationRequest}
            onRestoreDraftApplied={() => setReferenceDraft(null)}
            onOpenTask={(id) => { if (openInNewTab) openInNewTab(paths.issueDetail(id), undefined, { activate: true }); else push(paths.issueDetail(id)); }}
            onShowMessage={(message, tone) => toast[tone ?? "info"](message)} />
        </div>}
        </LocalSideWorkspace>
      </ResizablePanel>
    </ResizablePanelGroup>
  </div>;
});
