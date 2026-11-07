import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { observer } from "mobx-react-lite";
import { conversationService } from "../platform/conversation-services";
import {
  localV2Error,
  LocalV2RequestError
} from "@multica/conversation-client/services";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { LocalRuntimeNotice } from "./local-runtime-notice";
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState
} from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Archive,
  ChevronDown,
  ChevronRight,
  Copy,
  FolderKanban,
  FolderMinus,
  FolderOpen,
  FolderPlus,
  GitFork,
  Loader2,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Plus,
  Square
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Input } from "@multica/ui/components/ui/input";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle
} from "@multica/ui/components/ui/resizable";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem
} from "@multica/ui/components/ui/dropdown-menu";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent,
  ContextMenuRadioGroup,
  ContextMenuRadioItem
} from "@multica/ui/components/ui/context-menu";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter
} from "@multica/ui/components/ui/dialog";
import { copyText } from "@multica/ui/lib/clipboard";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { useAuthStore } from "@multica/core/auth";
import {
  useCurrentWorkspace,
  useRequiredWorkspaceSlug,
  useWorkspacePaths
} from "@multica/core/paths";
import { getCurrentSlug } from "@multica/core/platform";
import { projectListOptions } from "@multica/core/projects/queries";
import { useNavigation } from "@multica/views/navigation";
import { useLocale, useT } from "@multica/views/i18n";
import { TuttiAgentGuiPanel } from "../components/tutti-agent-gui-panel";
import type { ParentServiceOptions } from "../platform/agent-gui-parent-service";
import type {
  LocalFolderProject,
  LocalProviderId
} from "@multica/core/local-conversation/protocol";
import { isAutomaticLocalDirectory } from "@multica/tutti-agent-gui-host/local-projects";
import {
  createLocalConversationParentService,
  localConversationKey,
  localFolderProjectsQuery,
  localProvidersQuery,
  localTargetId,
  type LocalFolderProjectList,
  type LocalProviderList
} from "../platform/local-conversation-service";
import { useLocalConversationPages } from "../platform/local-conversation-pages";
import {
  isDefaultFolderProject,
  projectForDirectory,
  sessionEntry,
  type LocalSessionEntry
} from "../platform/local-conversation-projects";
import { LocalConversationSearch } from "./local-conversation-search";
import { LocalConversationListMenu } from "./local-conversation-import";
import {
  LocalSideWorkspace,
  type SideWorkspaceHandle
} from "./local-side-workspace";
import { PanelRightOpen } from "lucide-react";
import type { ComponentProps } from "react";
type RestoreDraftRequest = NonNullable<
  ComponentProps<typeof TuttiAgentGuiPanel>["restoreDraftRequest"]
>;
import {
  projectVisibilityScopeKey,
  useProjectVisibilityStore
} from "../stores/local-conversation-project-visibility";
import {
  SESSION_LIST_MIN_WIDTH,
  SESSION_LIST_MAX_WIDTH,
  useConversationLayoutStore
} from "../stores/local-conversation-layout";
import {
  localV2Transport,
  setRemoteFolderChooser
} from "../platform/local-v2-transport";
import {
  LocalDeviceProvider,
  useLocalDevice,
  type LocalDevice
} from "../platform/local-device";
import { deviceName, useLinkedDevices } from "../platform/linked-devices";
import { issueConversationDraftKey, useIssueConversationStore } from "../stores/issue-conversation-store";
import { pickRemoteFolder, RemoteFolderPicker } from "./remote-folder-picker";
import { LocalConversationSessionRow } from "./local-conversation-session-row";
import { useDocumentTitle } from "../hooks/use-document-title";
import { useConversationFocus } from "../platform/use-conversation-focus";
export const LocalConversationFlowPage = observer(
  LocalConversationFlowPageRender
);

const EMPTY_PROJECT_IDS: string[] = [];
function folderProject(project: {
  id: string;
  title: string;
  path: string;
  created_at: string;
}): LocalFolderProject {
  return {
    id: project.id,
    title: project.title,
    path: project.path,
    createdAt: project.created_at
  };
}

/** A daemon without the local v2 API answers its routes with 404. */
function localProjectsRequireRestart(error: unknown): boolean {
  return error instanceof LocalV2RequestError && error.kind === "http_404";
}

const DEVICE_ROUTE = /\/conversations\/devices\/([^/?#]+)/;

// A folder the user picked on another device's page, applied by main.
setRemoteFolderChooser(async (deviceId, scope, request) => {
  const path = await pickRemoteFolder(deviceId, scope, request);
  return path === null
    ? null
    : window.desktopAPI.localV2.chooseRemote(deviceId, scope, request, path);
});

/**
 * Conversations of this device, or of another device of the account over
 * Device link (`/conversations/devices/{peer}`). The page is the same; only
 * where data comes from and where agents run changes.
 */
function LocalConversationFlowPageRender() {
  const { pathname } = useNavigation();
  const match = DEVICE_ROUTE.exec(pathname);
  const deviceId = match ? decodeURIComponent(match[1]!) : "local";
  const devices = useLinkedDevices(deviceId !== "local");
  const name = deviceId === "local" ? "" : deviceName(devices, deviceId);
  const device = useMemo<LocalDevice>(
    () => ({ id: deviceId, name, transport: localV2Transport(deviceId) }),
    [deviceId, name]
  );
  return (
    <LocalDeviceProvider device={device}>
      <LocalConversationFlow key={deviceId} />
    </LocalDeviceProvider>
  );
}

const LocalConversationFlow = observer(function LocalConversationFlow() {
  const workspace = useCurrentWorkspace();
  const slug = useRequiredWorkspaceSlug();
  const userId = useAuthStore((state) => state.user?.id);
  const paths = useWorkspacePaths();
  const { pathname, searchParams, replace, openInNewTab, push } =
    useNavigation();
  const chinese = useLocale().startsWith("zh");
  const { t } = useT("chat");
  const compact = useIsCompact();
  const [sessionListWidth] = useState(
    () => useConversationLayoutStore.getState().sessionListWidth
  );
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
      if (width !== undefined && width !== before)
        useConversationLayoutStore.getState().setSessionListWidth(width);
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
  const rememberSessionResizeCallback = usePersistCallback(
    rememberSessionResize
  );
  const qc = useQueryClient();
  const device = useLocalDevice();
  const { transport } = device;
  const remote = device.id !== "local";
  const flowPath = remote
    ? paths.conversationFlowDevice(device.id)
    : paths.conversationFlow();
  const { t: tLayout } = useT("layout");
  useDocumentTitle(
    remote
      ? tLayout(($) => $.nav.conversation_flow_device, { device: device.name })
      : tLayout(($) => $.nav.conversation_flow)
  );
  const sideWorkspace = useRef<SideWorkspaceHandle>(null);
  const [sideOpen, setSideOpen] = useState(false);
  const [referenceDraft, setReferenceDraft] =
    useState<RestoreDraftRequest | null>(null);
  const [locateRequest, setLocateRequest] = useState<{
    sessionId: string;
    turnId?: string;
    quote?: string;
    sequence: number;
  } | null>(null);
  const scopeUserIdValue1 = userId || "";
  const scopeWorkspaceIdValue2 = workspace?.id || "";
  const scope = useMemo(
    () => ({ userId: scopeUserIdValue1, workspaceId: scopeWorkspaceIdValue2 }),
    [scopeUserIdValue1, scopeWorkspaceIdValue2]
  );
  const v2 = conversationService(transport, scope);
  const visibilityScope = projectVisibilityScopeKey(scope);
  const hiddenProjectPreferences = useProjectVisibilityStore(
    (state) => state.hiddenByScope[visibilityScope] ?? EMPTY_PROJECT_IDS
  );
  const setProjectHidden = useProjectVisibilityStore(
    (state) => state.setProjectHidden
  );
  const [archived, setArchived] = useState(false);
  const [showList, setShowList] = useState(false);
  const [newConversation, setNewConversation] = useState<{
    scope: string;
    request: NonNullable<
      ComponentProps<typeof TuttiAgentGuiPanel>["newConversationRequest"]
    >;
  }>();
  const newConversationRequest =
    newConversation?.scope === visibilityScope
      ? newConversation.request
      : undefined;
  const focusIntentSequence = useRef(0);
  const [addingProject, setAddingProject] = useState(false);
  const [forkingId, setForkingId] = useState<string | null>(null);
  const forking = forkingId !== null;
  const [updatingId, setUpdatingId] = useState<string | null>(null);
  const [renameTarget, setRenameTarget] = useState<Pick<
    LocalSessionEntry,
    "id" | "title"
  > | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [renameError, setRenameError] = useState<string | null>(null);
  const renameInput = useRef<HTMLInputElement>(null);
  const renameReturnFocus = useRef<HTMLAnchorElement | null>(null);
  const sessionLinks = useRef(new Map<string, HTMLAnchorElement>());
  const renameErrorId = useId();
  const [contextSelection, setContextSelection] = useState("");
  const [collapsedProjects, setCollapsedProjects] = useState<Set<string>>(
    () => new Set()
  );
  const [focusSequence, setFocusSequence] = useState(0);
  const providers = useQuery({
    ...localProvidersQuery(v2, device.id),
    select: (value: LocalProviderList) => value.providers,
    enabled: !!workspace && !!userId
  });
  const projectQuery = useQuery({
    ...projectListOptions(scope.workspaceId),
    enabled: !!workspace && !!userId
  });
  const projectData = projectQuery.data;
  const currentWorkspaceId = scope.workspaceId;
  const projects = useMemo(
    () =>
      (projectData ?? []).filter(
        (project) => project.workspace_id === currentWorkspaceId
      ),
    [projectData, currentWorkspaceId]
  );
  const folderQuery = useQuery({
    ...localFolderProjectsQuery(v2, device.id),
    select: (value: LocalFolderProjectList) =>
      value.projects.map(folderProject),
    enabled: !!workspace && !!userId,
    retry: (count, error) => !localProjectsRequireRestart(error) && count < 2
  });
  useEffect(() => {
    if (!scope.userId || !scope.workspaceId) return;
    const key = localConversationKey(
      { userId: scope.userId, workspaceId: scope.workspaceId },
      device.id
    );
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
      if (
        online &&
        value.userId === scope.userId &&
        value.workspaceId === scope.workspaceId
      )
        refresh();
    });
    return () => {
      stopAvailability();
      stopConnection();
    };
  }, [transport, qc, device.id, scope.userId, scope.workspaceId]);
  const folderProjectData = folderQuery.data;
  const folderProjects = useMemo(
    () => folderProjectData ?? [],
    [folderProjectData]
  );
  const hiddenProjectIds = useMemo(
    () =>
      hiddenProjectPreferences.filter((id) =>
        folderProjects.some((project) => project.id === id)
      ),
    [hiddenProjectPreferences, folderProjects]
  );
  const searchProjects = useMemo(
    () => folderProjects.filter((project) => !hiddenProjectIds.includes(project.id)),
    [folderProjects, hiddenProjectIds]
  );
  const selectedId = searchParams.get("session");
  const issueDraftKey = issueConversationDraftKey(scope, device.id, selectedId ?? "");
  const issueDraft = useIssueConversationStore((state) => state.drafts[issueDraftKey]);
  const sessions = useLocalConversationPages(
    transport,
    scope,
    folderProjects,
    hiddenProjectIds,
    archived,
    "",
    !!workspace && !!userId
  );
  // The list already knows every conversation; only an ID it lacks needs its snapshot read here.
  const selectedSummary = selectedId ? v2.list.summary(selectedId) : undefined;
  const selectedQuery = useQuery({
    queryKey: [
      ...localConversationKey(scope, device.id),
      "selected",
      selectedId
    ],
    queryFn: async () =>
      sessionEntry((await v2.snapshot(selectedId!)).conversation),
    enabled:
      !!workspace &&
      !!userId &&
      !!selectedId &&
      !selectedSummary &&
      !sessions.isPending,
    retry: false,
    refetchOnMount: "always"
  });
  const selectedData = selectedQuery.data;
  const selected = useMemo(
    () => (selectedSummary ? sessionEntry(selectedSummary) : selectedData),
    [selectedSummary, selectedData]
  );
  const selectedRef = useRef(selectedId);
  selectedRef.current = selectedId;
  const provider =
    selected?.provider ??
    (searchParams.get("provider") === "claude" ? "claude" : "codex");
  const active = pathname === flowPath && getCurrentSlug() === slug;
  useConversationFocus(v2, selectedId, active);
  const intent = `${userId}:${workspace?.id}:${selectedId}:${provider}`;
  const currentIntent = useRef(intent);
  currentIntent.current = intent;
  const latest = useRef({
    select: (_id: string | null, _provider?: LocalProviderId) => {},
    locate: (_id: string, _turnId?: string, _quote?: string) => {}
  });
  const select = (id: string | null, choice: LocalProviderId = provider) => {
    const query = new URLSearchParams({ provider: choice });
    if (id) query.set("session", id);
    replace(`${flowPath}?${query}`);
    setShowList(false);
  };
  const selectCallback = usePersistCallback(select);
  latest.current.select = select;
  latest.current.locate = (id, turnId, quote) => {
    const source =
      sessions.data?.find((entry) => entry.id === id) ??
      (selected?.id === id ? selected : undefined);
    const sequence = ++focusIntentSequence.current;
    const startedIntent = currentIntent.current;
    const locate = (entry: LocalSessionEntry) => {
      if (
        sequence !== focusIntentSequence.current ||
        currentIntent.current !== startedIntent
      )
        return;
      setArchived(entry.archived);
      const project = projectForDirectory(folderProjects, entry.cwd);
      setCollapsedProjects((previous) => {
        const next = new Set(previous);
        next.delete(project && !isDefaultFolderProject(project) ? project.id : "unassigned");
        return next;
      });
      select(id, entry.provider);
      setLocateRequest({ sessionId: id, turnId, quote, sequence });
    };
    if (source) locate(source);
    else
      void qc
        .fetchQuery({
          queryKey: [...localConversationKey(scope, device.id), "selected", id],
          queryFn: async () =>
            sessionEntry((await v2.snapshot(id)).conversation)
        })
        .then(locate)
        .catch((error: unknown) =>
          toast.error(error instanceof Error ? error.message : String(error))
        );
  };
  const factory = useCallback(
    (options: ParentServiceOptions) =>
      createLocalConversationParentService(
        {
          ...options,
          locale: chinese ? "zh-CN" : "en",
          getSelectionIntent: () => currentIntent.current,
          getSelectedSessionId: () => selectedRef.current,
          onLocalSelect: (id, choice) => latest.current.select(id, choice),
          onOpenSide: (id, sourceId) =>
            sideWorkspace.current?.openSide(id, sourceId),
          onOpenResource: (id, url) => {
            void sideWorkspace.current
              ?.openResource(id, url, id)
              .catch((error: unknown) =>
                toast.error(
                  error instanceof Error ? error.message : String(error)
                )
              );
          },
          onLocateSource: (sessionId, turnId, quote) =>
            latest.current.locate(sessionId, turnId, quote)
        },
        transport
      ),
    [transport, chinese]
  );
  const factoryCallback = factory;
  useEffect(() => {
    // Restored tabs may still contain an old cloud chat ID. Cloud history remains in Chat.
    // A stale list or the outgoing page must not clear another tab's selection.
    if (
      active &&
      selectedQuery.isError &&
      selectedQuery.error instanceof LocalV2RequestError &&
      selectedQuery.error.kind === "not_found" &&
      selectedId
    )
      latest.current.select(null, provider);
  }, [
    active,
    selectedQuery.isError,
    selectedQuery.error,
    selectedId,
    provider
  ]);
  const labels = useMemo(
    () =>
      chinese
        ? {
            sessions: "本机会话",
            new: "新建会话",
            search: "搜索会话",
            empty: "还没有本机会话",
            noResults: "没有匹配的会话",
            retry: "重试",
            loading: "正在加载…",
            archive: "归档",
            history: "返回会话",
            pinned: "置顶",
            unpin: "取消置顶",
            directory: "选择工作目录",
            newTitle: "新建会话",
            storage: "历史保存在本机",
            directoryLocked: "工作目录在首次发送后固定；如需更换，请新建会话。",
            unavailable: "请先在本机安装 Codex 或 Claude Code",
            failed: "操作失败，请重试",
            fork: "分叉",
            forking: "正在分叉…",
            forkHint: "从当前历史创建独立会话",
            forkWaiting: "执行结束后可分叉",
            forkEmpty: "发送消息后可分叉"
          }
        : {
            sessions: "Local sessions",
            new: "New conversation",
            search: "Search conversations",
            empty: "No local conversations yet",
            noResults: "No matching conversations",
            retry: "Retry",
            loading: "Loading…",
            archive: "Archive",
            history: "Back to sessions",
            pinned: "Pin",
            unpin: "Unpin",
            directory: "Choose working directory",
            newTitle: "New conversation",
            storage: "History saved on this computer",
            directoryLocked:
              "The working directory is fixed after the first message. Start a new conversation to change it.",
            unavailable: "Install Codex or Claude Code on this computer",
            failed: "Could not update the conversation",
            fork: "Fork",
            forking: "Forking…",
            forkHint: "Create an independent conversation from this history",
            forkWaiting: "Wait for the current execution to finish",
            forkEmpty: "Send a message before forking"
          },
    [chinese]
  );
  const report = (error: unknown) =>
    toast.error(error instanceof Error ? error.message : labels.failed);
  const reportCallback = usePersistCallback(report);
  const canCreate = providers.data?.some((entry) => entry.available) ?? false;
  const providerLabels = chinese
    ? { checking: "正在检测本机运行器…", failed: "运行器检测失败" }
    : {
        checking: "Checking local providers…",
        failed: "Could not detect providers"
      };
  const createDisabledReason = providers.isPending
    ? providerLabels.checking
    : providers.isError
      ? providerLabels.failed
      : labels.unavailable;
  const renameLabels = useMemo(
    () =>
      chinese
        ? {
            action: "重命名",
            title: "重命名会话",
            name: "会话名称",
            save: "保存",
            saving: "正在保存...",
            cancel: "取消"
          }
        : {
            action: "Rename",
            title: "Rename conversation",
            name: "Conversation name",
            save: "Save",
            saving: "Saving...",
            cancel: "Cancel"
          },
    [chinese]
  );
  const projectLabels = useMemo(
    () =>
      chinese
        ? {
            unassigned: "未分配项目",
            unavailable: "项目不可用",
            actions: "会话操作",
            move: "关联工作区项目",
            noWorkspaceProject: "不关联工作区项目",
            create: "新建项目会话",
            new: t(($) => $.desktop.local_conversation.add_directory),
            empty: "暂无会话",
            loadFailed: "项目加载失败",
            folderLoadFailed: "本地项目加载失败",
            manage: "项目操作",
            remove: "从列表移除",
            removed: "已移除项目",
            restore: "恢复项目",
            removedNotice: "已从列表移除",
            undo: "撤销"
          }
        : {
            unassigned: "No project",
            unavailable: "Unavailable project",
            actions: "Conversation actions",
            move: "Link workspace project",
            noWorkspaceProject: "No workspace project",
            create: "New conversation in project",
            new: t(($) => $.desktop.local_conversation.add_directory),
            empty: "No conversations",
            loadFailed: "Could not load projects",
            folderLoadFailed: "Could not load local projects",
            manage: "Project actions",
            remove: "Remove from list",
            removed: "Removed projects",
            restore: "Restore project",
            removedNotice: "Project removed from list",
            undo: "Undo"
          },
    [chinese, t]
  );
  const folderRestartMessage = chinese
    ? "本地项目功能已更新，请重启桌面应用"
    : "Local projects have been updated. Restart the desktop app.";
  const groups = sessions.groups;
  const addProject = async () => {
    if (addingProject) return;
    setAddingProject(true);
    const startedIntent = intent;
    try {
      const added = (await transport.choose(scope, {
        purpose: "folder_project",
        ...(folderProjects.at(-1)
          ? { defaultPath: folderProjects.at(-1)!.path }
          : {})
      })) as {
        id: string;
        title: string;
        path: string;
        created_at: string;
      } | null;
      if (!added) return;
      const project = folderProject(added);
      await qc.invalidateQueries({
        queryKey: localConversationKey(scope, device.id)
      });
      if (currentIntent.current === startedIntent) {
        setArchived(false);
        setProjectHidden(scope, project.id, false);
        setCollapsedProjects((previous) => {
          const next = new Set(previous);
          next.delete(project.id);
          return next;
        });
      }
    } catch (raw) {
      const error = localV2Error(raw);
      if (localProjectsRequireRestart(error)) toast.error(folderRestartMessage);
      else report(error);
    } finally {
      setAddingProject(false);
    }
  };
  const addProjectCallback = usePersistCallback(addProject);
  const toggleArchiveCallback = usePersistCallback(() => setArchived((value) => !value));
  const restoreProject = (projectId: string) => {
    setProjectHidden(scope, projectId, false);
    setArchived(false);
    setCollapsedProjects((previous) => {
      const next = new Set(previous);
      next.delete(projectId);
      return next;
    });
  };
  const restoreProjectCallback = usePersistCallback(restoreProject);
  const removeProject = (projectId: string) => {
    setProjectHidden(scope, projectId, true);
    toast.success(projectLabels.removedNotice, {
      action: {
        label: projectLabels.undo,
        onClick: () => setProjectHidden(scope, projectId, false)
      }
    });
  };
  const removeProjectCallback = usePersistCallback(removeProject);
  const defaultFolderProjectId =
    projectForDirectory(
      folderProjects,
      selected?.cwd ?? newConversationRequest?.projectPath ?? ""
    )?.id ?? null;
  const create = (folderProjectId: string | null = defaultFolderProjectId) => {
    const target =
      providers.data?.find(
        (entry) => entry.id === provider && entry.available
      ) ?? providers.data?.find((entry) => entry.available);
    if (!target) return;
    const sequence = ++focusIntentSequence.current;
    setNewConversation({
      scope: visibilityScope,
      request: {
        sequence,
        projectPath:
          folderProjects.find((project) => project.id === folderProjectId)
            ?.path ?? folderProjects.find(isDefaultFolderProject)?.path ?? null
      }
    });
    setArchived(false);
    setFocusSequence(sequence);
    latest.current.select(null, target.id);
  };
  const createCallback = usePersistCallback(create);
  const update = async (
    params: {
      id: string;
      title?: string;
      pinned?: boolean;
      archived?: boolean;
      projectId?: string | null;
    },
    onError: (error: unknown) => void = report
  ) => {
    if (updatingId) return false;
    setUpdatingId(params.id);
    const startedIntent = intent;
    try {
      const { id, projectId, ...fields } = params;
      v2.receiveSummary(
        localV2SummarySchema.parse(
          await v2.command(id, {
            type: "update",
            ...fields,
            ...(projectId !== undefined ? { project_id: projectId } : {})
          })
        )
      );
      await qc.invalidateQueries({
        queryKey: localConversationKey(scope, device.id)
      });
      if (
        params.archived !== undefined &&
        selectedId === params.id &&
        currentIntent.current === startedIntent
      )
        latest.current.select(null, provider);
      return true;
    } catch (error) {
      onError(error);
      return false;
    } finally {
      setUpdatingId(null);
    }
  };
  const updateCallback = usePersistCallback(update);
  const startRename = (entry: LocalSessionEntry) => {
    renameReturnFocus.current = sessionLinks.current.get(entry.id) ?? null;
    setRenameDraft(entry.title);
    setRenameError(null);
    setRenameTarget({ id: entry.id, title: entry.title });
  };
  const startRenameCallback = usePersistCallback(startRename);
  const stopBackground = async (entry: LocalSessionEntry) => {
    if (updatingId) return;
    setUpdatingId(entry.id);
    try {
      await v2.command(entry.id, { type: "stop_background_tasks" });
    } catch (error) {
      report(error);
    } finally {
      setUpdatingId(null);
    }
  };
  const stopBackgroundCallback = usePersistCallback(stopBackground);
  const rename = async () => {
    const title = renameDraft.trim();
    if (!renameTarget || updatingId || !title || title.length > 256) return;
    if (title === renameTarget.title) {
      setRenameTarget(null);
      return;
    }
    setRenameError(null);
    if (
      await update({ id: renameTarget.id, title }, (error) =>
        setRenameError(error instanceof Error ? error.message : labels.failed)
      )
    )
      setRenameTarget(null);
  };
  const renameCallback = usePersistCallback(rename);
  const fork = async (entry = selected) => {
    if (forking || !entry || !entry.turnCount || entry.running) return;
    setForkingId(entry.id);
    const startedIntent = intent;
    try {
      const summary = localV2SummarySchema.parse(
        await v2.command(entry.id, {
          type: "fork",
          request_id: crypto.randomUUID()
        })
      );
      v2.receiveSummary(summary);
      const created = sessionEntry(summary);
      if (currentIntent.current === startedIntent) {
        setArchived(false);
        setFocusSequence(++focusIntentSequence.current);
        latest.current.select(created.id, created.provider);
      }
    } catch (error) {
      report(error);
    } finally {
      setForkingId(null);
    }
  };
  const forkCallback = usePersistCallback(fork);
  // Internal execution folders, including those inherited by forks, are not user-bound folders.
  const automaticDirectory = isAutomaticLocalDirectory(selected?.cwd ?? "");
  const directoryName = automaticDirectory
    ? projectLabels.unassigned
    : selected?.cwd.split(/[\\/]/).filter(Boolean).at(-1) || labels.directory;
  const renderSessionActions = useMemo(
    () =>
      function renderSessionActions(
        entry: LocalSessionEntry,
        contextMenu: boolean
      ) {
        const Sub = contextMenu ? ContextMenuSub : DropdownMenuSub;
        const SubTrigger = contextMenu
          ? ContextMenuSubTrigger
          : DropdownMenuSubTrigger;
        const SubContent = contextMenu
          ? ContextMenuSubContent
          : DropdownMenuSubContent;
        const RadioGroup = contextMenu
          ? ContextMenuRadioGroup
          : DropdownMenuRadioGroup;
        const RadioItem = contextMenu
          ? ContextMenuRadioItem
          : DropdownMenuRadioItem;
        const Item = contextMenu ? ContextMenuItem : DropdownMenuItem;
        const Separator = contextMenu
          ? ContextMenuSeparator
          : DropdownMenuSeparator;
        const disabled = updatingId !== null;
        const entryRunning = entry.running;
        return (
          <>
            <ItemBinding1
              Item={Item}
              disabled={disabled}
              startRename={startRenameCallback}
              entry={entry}
              renameLabels={renameLabels}
            />
            <ItemBinding2
              Item={Item}
              disabled={disabled}
              forking={forking}
              entryRunning={entryRunning}
              entry={entry}
              forkingId={forkingId}
              labels={labels}
              fork={forkCallback}
            />
            <ItemBinding3
              Item={Item}
              disabled={disabled}
              update={updateCallback}
              entry={entry}
              labels={labels}
            />
            {entry.backgroundTasks > 0 && (
              <ItemBinding4
                Item={Item}
                disabled={disabled}
                stopBackground={stopBackgroundCallback}
                entry={entry}
                chinese={chinese}
              />
            )}
            <Separator />
            <Sub>
              <SubTrigger disabled={disabled}>
                <FolderKanban />
                {projectLabels.move}
              </SubTrigger>
              <SubContent className="max-h-80 max-w-72 overflow-y-auto">
                <RadioGroupBinding5
                  RadioGroup={RadioGroup}
                  entry={entry}
                  update={updateCallback}
                  RadioItem={RadioItem}
                  disabled={disabled}
                  projectLabels={projectLabels}
                  projects={projects}
                />
                {projectQuery.isPending && (
                  <Item disabled>{labels.loading}</Item>
                )}
                {projectQuery.isError && (
                  <ItemBinding6
                    Item={Item}
                    disabled={disabled}
                    projectQuery={projectQuery}
                    projectLabels={projectLabels}
                    labels={labels}
                  />
                )}
              </SubContent>
            </Sub>
            <ItemBinding7 Item={Item} entry={entry} chinese={chinese} />
            <Separator />
            <ItemBinding8
              Item={Item}
              disabled={disabled}
              update={updateCallback}
              entry={entry}
              archived={archived}
              labels={labels}
            />
          </>
        );
      },
    [
      updatingId,
      startRenameCallback,
      renameLabels,
      forking,
      forkingId,
      labels,
      forkCallback,
      updateCallback,
      stopBackgroundCallback,
      chinese,
      projectLabels,
      projects,
      projectQuery,
      archived
    ]
  );
  const renderSessionActionsCallback = renderSessionActions;
  const renderSession = (entry: LocalSessionEntry) => {
    return (
      <ContextMenu key={entry.id}>
        <LocalConversationSessionRow
          id={entry.id}
          model={sessions.model}
          setContextSelection={setContextSelection}
          sessionLinks={sessionLinks}
          flowPath={flowPath}
          selected={selectedId === entry.id}
          select={selectCallback}
          updatingId={updatingId}
          labels={labels}
          t={t}
          projectLabels={projectLabels}
          renderSessionActions={renderSessionActionsCallback}
          chinese={chinese}
          remote={remote}
          v2={v2}
        />
        <ContextMenuContent
          aria-label={`${projectLabels.actions}：${entry.title}`}
        >
          {contextSelection.trim() && (
            <>
              <ContextMenuItemBinding11
                contextSelection={contextSelection}
                chinese={chinese}
              />
              <ContextMenuSeparator />
            </>
          )}
          {renderSessionActions(entry, true)}
        </ContextMenuContent>
      </ContextMenu>
    );
  };
  if (!workspace || !userId) return null;
  return (
    <div
      className="flex min-h-0 min-w-0 flex-1"
      data-testid="local-conversation-flow"
    >
      <DialogBinding1
        renameTarget={renameTarget}
        updatingId={updatingId}
        setRenameTarget={setRenameTarget}
        renameInput={renameInput}
        renameReturnFocus={renameReturnFocus}
        rename={renameCallback}
        renameLabels={renameLabels}
        renameDraft={renameDraft}
        renameError={renameError}
        renameErrorId={renameErrorId}
        setRenameDraft={setRenameDraft}
        setRenameError={setRenameError}
      />
      <ResizablePanelGroup
        orientation="horizontal"
        disabled={compact}
        className={`relative min-h-0 min-w-0 flex-1 [&>[hidden]]:!hidden ${compact ? "[&>[data-panel]]:absolute [&>[data-panel]]:inset-0" : ""}`}
      >
        <ResizablePanel
          id="local-sessions"
          elementRef={sessionListPanel}
          defaultSize={sessionListWidth}
          minSize={SESSION_LIST_MIN_WIDTH}
          maxSize={SESSION_LIST_MAX_WIDTH}
          groupResizeBehavior="preserve-pixel-size"
          hidden={compact && !showList}
        >
          <div
            className="flex h-full min-h-0 min-w-0 flex-col border-r"
            data-testid="local-conversation-sessions"
          >
            <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
              <h1 className="min-w-0 flex-1 truncate text-body font-semibold">
                {labels.sessions}
              </h1>
              <div className="flex shrink-0 items-center gap-1">
                <LocalConversationSearch
                  key={`${scope.userId}:${scope.workspaceId}:${device.id}`}
                  sessions={sessions.data}
                  projects={searchProjects}
                  archived={archived}
                  pending={sessions.isPending}
                  error={sessions.error}
                  retry={sessions.refetch}
                  select={selectCallback}
                  labels={labels}
                  unassigned={projectLabels.unassigned}
                />
                <IconButtonBinding15
                  projectLabels={projectLabels}
                  addingProject={addingProject}
                  addProject={addProjectCallback}
                />
                <LocalConversationListMenuBinding14
                  key={`${scope.userId}:${scope.workspaceId}`}
                  scope={scope}
                  transport={transport}
                  projects={projects}
                  chinese={chinese}
                  qc={qc}
                  device={device}
                  archived={archived}
                  onToggleArchive={toggleArchiveCallback}
                />
              </div>
            </div>
            <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto px-3 pb-2 pt-3">
              {providers.isError && (
                <div
                  role="status"
                  aria-label={providerLabels.failed}
                  className="flex items-center justify-between gap-2 px-3 py-1 text-caption text-muted-foreground"
                >
                  <span>{providerLabels.failed}</span>
                  <ButtonBinding18 providers={providers} labels={labels} />
                </div>
              )}
              {folderQuery.isError && (
                <div
                  role="status"
                  className="flex items-center justify-between gap-2 px-3 py-1 text-caption text-muted-foreground"
                >
                  <span>
                    {localProjectsRequireRestart(folderQuery.error)
                      ? folderRestartMessage
                      : projectLabels.folderLoadFailed}
                  </span>
                  <ButtonBinding19 folderQuery={folderQuery} labels={labels} />
                </div>
              )}
              {sessions.isPending ? (
                <p
                  role="status"
                  className="p-4 text-body text-muted-foreground"
                >
                  {labels.loading}
                </p>
              ) : sessions.isError ? (
                <div role="alert" className="p-4">
                  <ButtonBinding20 sessions={sessions} labels={labels} />
                </div>
              ) : groups.length ? (
                groups.map((group) => {
                  const key = group.id ?? "unassigned";
                  const expanded = !collapsedProjects.has(key);
                  const title =
                    group.id === null
                      ? projectLabels.unassigned
                      : (group.project?.title ?? projectLabels.unavailable);
                  return (
                    <section
                      key={key}
                      className="flex shrink-0 flex-col gap-2"
                      aria-label={title}
                    >
                      <div className="flex items-center gap-1 rounded-md bg-muted pr-1">
                        <ButtonBinding21
                          title={title}
                          group={group}
                          expanded={expanded}
                          setCollapsedProjects={setCollapsedProjects}
                          bindingKey={key}
                        />
                        <div className="flex shrink-0 items-center">
                          {!archived &&
                            (group.id === null || group.project) && (
                              <IconButtonBinding22
                                projectLabels={projectLabels}
                                title={title}
                                canCreate={canCreate}
                                createDisabledReason={createDisabledReason}
                                create={createCallback}
                                group={group}
                              />
                            )}
                          {group.id !== null && (
                            <DropdownMenu>
                              <DropdownMenuTriggerBinding23
                                projectLabels={projectLabels}
                                title={title}
                              />
                              <DropdownMenuContent align="end">
                                <DropdownMenuItemBinding24
                                  removeProject={removeProjectCallback}
                                  bindingKey={key}
                                  projectLabels={projectLabels}
                                />
                              </DropdownMenuContent>
                            </DropdownMenu>
                          )}
                        </div>
                      </div>
                      {expanded && (
                        <>
                          {group.sessions.length ? (
                            <div className="flex flex-col gap-1">
                              {group.sessions.map(renderSession)}
                            </div>
                          ) : (
                            <p className="px-3 py-2 text-caption text-muted-foreground">
                              {projectLabels.empty}
                            </p>
                          )}
                          {group.nextCursor && (
                            <ButtonBinding25
                              sessions={sessions}
                              group={group}
                              report={reportCallback}
                              labels={labels}
                              chinese={chinese}
                            />
                          )}
                        </>
                      )}
                    </section>
                  );
                })
              ) : (
                <p className="px-3 py-8 text-center text-body text-muted-foreground">
                  {labels.empty}
                </p>
              )}
            </div>
            {hiddenProjectIds.length > 0 && (
              <div className="shrink-0 px-3 pt-1">
                <DropdownMenu>
                  <DropdownMenuTriggerBinding26
                    projectLabels={projectLabels}
                    chinese={chinese}
                    hiddenProjectIds={hiddenProjectIds}
                  />
                  <DropdownMenuContent
                    side="top"
                    align="start"
                    aria-label={projectLabels.restore}
                    className="max-h-80 max-w-72 overflow-y-auto"
                  >
                    {hiddenProjectIds.map((id) => {
                      const title =
                        folderProjects.find((project) => project.id === id)
                          ?.title ??
                        `${projectLabels.unavailable} · ${id.slice(0, 8)}`;
                      return (
                        <DropdownMenuItemBinding27
                          key={id}
                          id={id}
                          projectLabels={projectLabels}
                          title={title}
                          restoreProject={restoreProjectCallback}
                        />
                      );
                    })}
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            )}
          </div>
        </ResizablePanel>
        <ResizableHandleBinding29
          compact={compact}
          chinese={chinese}
          rememberSessionResize={rememberSessionResizeCallback}
        />
        <ResizablePanel
          id="local-detail"
          minSize="40%"
          hidden={compact && showList}
        >
          <LocalSideWorkspaceBinding3
            key={`${workspace.id}:${userId}`}
            workspace={workspace}
            userId={userId}
            sideWorkspace={sideWorkspace}
            setSideOpen={setSideOpen}
            selectedId={selectedId}
            scope={scope}
            slug={slug}
            selected={selected}
            provider={provider}
            sessions={sessions}
            chinese={chinese}
            active={active}
            compact={compact}
            showList={showList}
            latest={latest}
            setReferenceDraft={setReferenceDraft}
            openInNewTab={openInNewTab}
            paths={paths}
            push={push}
            labels={labels}
            setShowList={setShowList}
            automaticDirectory={automaticDirectory}
            directoryName={directoryName}
            transport={transport}
            v2={v2}
            report={reportCallback}
            sideOpen={sideOpen}
            remote={remote}
            locateRequest={locateRequest}
            referenceDraft={referenceDraft ?? issueDraft ?? null}
            factory={factoryCallback}
            focusSequence={focusSequence}
            newConversationRequest={newConversationRequest}
          />
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
});

const ItemBinding1 = observer(function ItemBinding1({
  Item,
  disabled,
  startRename,
  entry,
  renameLabels
}: {
  Item: ({
    className,
    inset,
    variant,
    ...props
  }: import("@base-ui/react").MenuItem.Props & {
    inset?: boolean;
    variant?: "default" | "destructive";
  }) => import("react").JSX.Element;
  disabled: boolean;
  startRename: (entry: LocalSessionEntry) => void;
  entry: LocalSessionEntry;
  renameLabels: {
    action: string;
    title: string;
    name: string;
    save: string;
    saving: string;
    cancel: string;
  };
}) {
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Item>["onClick"]>
  >(() => startRename(entry));
  return (
    <Item disabled={disabled} onClick={onClickProp1}>
      <Pencil />
      {renameLabels.action}
    </Item>
  );
});

const ItemBinding2 = observer(function ItemBinding2({
  Item,
  disabled,
  forking,
  entryRunning,
  entry,
  forkingId,
  labels,
  fork
}: {
  Item: ({
    className,
    inset,
    variant,
    ...props
  }: import("@base-ui/react").MenuItem.Props & {
    inset?: boolean;
    variant?: "default" | "destructive";
  }) => import("react").JSX.Element;
  disabled: boolean;
  forking: boolean;
  entryRunning: boolean;
  entry: LocalSessionEntry;
  forkingId: string | null;
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
  fork: (entry?: LocalSessionEntry | undefined) => Promise<void>;
}) {
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Item>["onClick"]>
  >(() => {
    void fork(entry);
  });
  return (
    <Item
      disabled={disabled || forking || entryRunning || !entry.turnCount}
      aria-busy={forkingId === entry.id}
      title={
        entryRunning
          ? labels.forkWaiting
          : !entry.turnCount
            ? labels.forkEmpty
            : labels.forkHint
      }
      onClick={onClickProp2}
    >
      {forkingId === entry.id ? (
        <Loader2 className="animate-spin" />
      ) : (
        <GitFork />
      )}
      {forkingId === entry.id ? labels.forking : labels.fork}
    </Item>
  );
});

const ItemBinding3 = observer(function ItemBinding3({
  Item,
  disabled,
  update,
  entry,
  labels
}: {
  Item: ({
    className,
    inset,
    variant,
    ...props
  }: import("@base-ui/react").MenuItem.Props & {
    inset?: boolean;
    variant?: "default" | "destructive";
  }) => import("react").JSX.Element;
  disabled: boolean;
  update: (
    params: {
      id: string;
      title?: string;
      pinned?: boolean;
      archived?: boolean;
      projectId?: string | null;
    },
    onError?: (error: unknown) => void
  ) => Promise<boolean>;
  entry: LocalSessionEntry;
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
}) {
  const onClickProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Item>["onClick"]>
  >(() => {
    void update({ id: entry.id, pinned: !entry.pinned });
  });
  return (
    <Item disabled={disabled} onClick={onClickProp3}>
      {entry.pinned ? <PinOff /> : <Pin />}
      {entry.pinned ? labels.unpin : labels.pinned}
    </Item>
  );
});

const ItemBinding4 = observer(function ItemBinding4({
  Item,
  disabled,
  stopBackground,
  entry,
  chinese
}: {
  Item: ({
    className,
    inset,
    variant,
    ...props
  }: import("@base-ui/react").MenuItem.Props & {
    inset?: boolean;
    variant?: "default" | "destructive";
  }) => import("react").JSX.Element;
  disabled: boolean;
  stopBackground: (entry: LocalSessionEntry) => Promise<void>;
  entry: LocalSessionEntry;
  chinese: boolean;
}) {
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Item>["onClick"]>
  >(() => {
    void stopBackground(entry);
  });
  return (
    <Item disabled={disabled} onClick={onClickProp4}>
      <Square />
      {chinese ? "停止后台任务" : "Stop background tasks"}
    </Item>
  );
});

const RadioGroupBinding5 = observer(function RadioGroupBinding5({
  RadioGroup,
  entry,
  update,
  RadioItem,
  disabled,
  projectLabels,
  projects
}: {
  RadioGroup: ({
    ...props
  }: import("@base-ui/react").MenuRadioGroup.Props) => import("react").JSX.Element;
  entry: LocalSessionEntry;
  update: (
    params: {
      id: string;
      title?: string;
      pinned?: boolean;
      archived?: boolean;
      projectId?: string | null;
    },
    onError?: (error: unknown) => void
  ) => Promise<boolean>;
  RadioItem: ({
    className,
    children,
    inset,
    ...props
  }: import("@base-ui/react").MenuRadioItem.Props & {
    inset?: boolean;
  }) => import("react").JSX.Element;
  disabled: boolean;
  projectLabels: {
    unassigned: string;
    unavailable: string;
    actions: string;
    move: string;
    noWorkspaceProject: string;
    create: string;
    new: string;
    empty: string;
    loadFailed: string;
    folderLoadFailed: string;
    manage: string;
    remove: string;
    removed: string;
    restore: string;
    removedNotice: string;
    undo: string;
  };
  projects: import("../../../../../../packages/core/types/project").Project[];
}) {
  const onValueChangeProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof RadioGroup>["onValueChange"]>
  >((value) => {
    void update({
      id: entry.id,
      projectId: value === "__none__" ? null : value
    });
  });
  return (
    <RadioGroup
      value={entry.projectId ?? "__none__"}
      onValueChange={onValueChangeProp5}
    >
      <RadioItem disabled={disabled} value="__none__">
        {projectLabels.noWorkspaceProject}
      </RadioItem>
      {projects.map((project) => (
        <RadioItem key={project.id} disabled={disabled} value={project.id}>
          <span className="truncate">{project.title}</span>
        </RadioItem>
      ))}
    </RadioGroup>
  );
});

const ItemBinding6 = observer(function ItemBinding6({
  Item,
  disabled,
  projectQuery,
  projectLabels,
  labels
}: {
  Item: ({
    className,
    inset,
    variant,
    ...props
  }: import("@base-ui/react").MenuItem.Props & {
    inset?: boolean;
    variant?: "default" | "destructive";
  }) => import("react").JSX.Element;
  disabled: boolean;
  projectQuery:
    | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
        import("../../../../../../packages/core/types/project").Project[],
        Error
      >
    | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
        import("../../../../../../packages/core/types/project").Project[],
        Error
      >;
  projectLabels: {
    unassigned: string;
    unavailable: string;
    actions: string;
    move: string;
    noWorkspaceProject: string;
    create: string;
    new: string;
    empty: string;
    loadFailed: string;
    folderLoadFailed: string;
    manage: string;
    remove: string;
    removed: string;
    restore: string;
    removedNotice: string;
    undo: string;
  };
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
}) {
  const onClickProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Item>["onClick"]>
  >(() => {
    void projectQuery.refetch();
  });
  return (
    <Item disabled={disabled} onClick={onClickProp6}>
      {projectLabels.loadFailed} · {labels.retry}
    </Item>
  );
});

const ItemBinding7 = observer(function ItemBinding7({
  Item,
  entry,
  chinese
}: {
  Item: ({
    className,
    inset,
    variant,
    ...props
  }: import("@base-ui/react").MenuItem.Props & {
    inset?: boolean;
    variant?: "default" | "destructive";
  }) => import("react").JSX.Element;
  entry: LocalSessionEntry;
  chinese: boolean;
}) {
  const onClickProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Item>["onClick"]>
  >(() => {
    void copyText(entry.id).then((copied) => {
      if (copied)
        toast.success(chinese ? "已复制会话 ID" : "Conversation ID copied");
      else
        toast.error(
          chinese
            ? "无法复制会话 ID，请重试"
            : "Could not copy conversation ID. Try again."
        );
    });
  });
  return (
    <Item onClick={onClickProp7}>
      <Copy />
      {chinese ? "复制会话 ID" : "Copy conversation ID"}
    </Item>
  );
});

const ItemBinding8 = observer(function ItemBinding8({
  Item,
  disabled,
  update,
  entry,
  archived,
  labels
}: {
  Item: ({
    className,
    inset,
    variant,
    ...props
  }: import("@base-ui/react").MenuItem.Props & {
    inset?: boolean;
    variant?: "default" | "destructive";
  }) => import("react").JSX.Element;
  disabled: boolean;
  update: (
    params: {
      id: string;
      title?: string;
      pinned?: boolean;
      archived?: boolean;
      projectId?: string | null;
    },
    onError?: (error: unknown) => void
  ) => Promise<boolean>;
  entry: LocalSessionEntry;
  archived: boolean;
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
}) {
  const onClickProp8 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Item>["onClick"]>
  >(() => {
    void update({ id: entry.id, archived: !archived });
  });
  return (
    <Item disabled={disabled} onClick={onClickProp8}>
      <Archive />
      {archived ? labels.history : labels.archive}
    </Item>
  );
});

const ContextMenuItemBinding11 = observer(function ContextMenuItemBinding11({
  contextSelection,
  chinese
}: {
  contextSelection: string;
  chinese: boolean;
}) {
  const onClickProp12 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof ContextMenuItem>["onClick"]>
  >(() => {
    void copyText(contextSelection).then((copied) => {
      if (!copied)
        toast.error(
          chinese ? "无法复制文字，请重试" : "Could not copy text. Try again."
        );
    });
  });
  return (
    <ContextMenuItem onClick={onClickProp12}>
      <Copy />
      {chinese ? "复制所选文字" : "Copy selected text"}
    </ContextMenuItem>
  );
});

const InputBinding12 = observer(function InputBinding12({
  renameInput,
  renameDraft,
  updatingId,
  renameError,
  renameErrorId,
  setRenameDraft,
  setRenameError,
  ...slotProps
}: {
  renameInput: import("react").RefObject<HTMLInputElement | null>;
  renameDraft: string;
  updatingId: string | null;
  renameError: string | null;
  renameErrorId: string;
  setRenameDraft: import("react").Dispatch<
    import("react").SetStateAction<string>
  >;
  setRenameError: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    renameInput: import("react").RefObject<HTMLInputElement | null>;
    renameDraft: string;
    updatingId: string | null;
    renameError: string | null;
    renameErrorId: string;
    setRenameDraft: import("react").Dispatch<
      import("react").SetStateAction<string>
    >;
    setRenameError: import("react").Dispatch<
      import("react").SetStateAction<string | null>
    >;
  }
>) {
  const onFocusProp13 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Input>["onFocus"]>
  >((event) => event.currentTarget.select());
  const onChangeProp14 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Input>["onChange"]>
  >((event) => {
    setRenameDraft(event.target.value);
    setRenameError(null);
  });
  const onKeyDownProp15 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Input>["onKeyDown"]>
  >((event) => {
    if (
      event.key === "Enter" &&
      (event.nativeEvent.isComposing || event.keyCode === 229)
    )
      event.preventDefault();
  });
  return (
    <Slot.Root {...slotProps}>
      <Input
        ref={renameInput}
        value={renameDraft}
        maxLength={256}
        required
        disabled={updatingId !== null}
        aria-invalid={Boolean(renameError)}
        aria-describedby={renameError ? renameErrorId : undefined}
        onFocus={onFocusProp13}
        onChange={onChangeProp14}
        onKeyDown={onKeyDownProp15}
      />
    </Slot.Root>
  );
});

const ButtonBinding13 = observer(function ButtonBinding13({
  updatingId,
  setRenameTarget,
  renameLabels,
  ...slotProps
}: {
  updatingId: string | null;
  setRenameTarget: import("react").Dispatch<
    import("react").SetStateAction<Pick<
      LocalSessionEntry,
      "title" | "id"
    > | null>
  >;
  renameLabels: {
    action: string;
    title: string;
    name: string;
    save: string;
    saving: string;
    cancel: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    updatingId: string | null;
    setRenameTarget: import("react").Dispatch<
      import("react").SetStateAction<Pick<
        LocalSessionEntry,
        "title" | "id"
      > | null>
    >;
    renameLabels: {
      action: string;
      title: string;
      name: string;
      save: string;
      saving: string;
      cancel: string;
    };
  }
>) {
  const onClickProp16 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => setRenameTarget(null));
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        variant="outline"
        disabled={updatingId !== null}
        onClick={onClickProp16}
      >
        {renameLabels.cancel}
      </Button>
    </Slot.Root>
  );
});

const LocalConversationListMenuBinding14 = observer(
  function LocalConversationListMenuBinding14({
    scope,
    transport,
    projects,
    chinese,
    qc,
    device,
    archived,
    onToggleArchive
  }: {
    scope: { userId: string; workspaceId: string };
    transport: import("../platform/local-v2-transport").DirectLocalV2Transport;
    projects: import("../../../../../../packages/core/types/project").Project[];
    chinese: boolean;
    qc: import("@tanstack/react-query").QueryClient;
    device: LocalDevice;
    archived: boolean;
    onToggleArchive(): void;
  }) {
    const onImportedProp17 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof LocalConversationListMenu>["onImported"]
      >
    >(() => {
      void qc.invalidateQueries({
        queryKey: localConversationKey(scope, device.id)
      });
    });
    return (
      <LocalConversationListMenu
        key={`${scope.userId}:${scope.workspaceId}`}
        scope={scope}
        client={transport}
        projects={projects}
        chinese={chinese}
        onImported={onImportedProp17}
        archived={archived}
        onToggleArchive={onToggleArchive}
      />
    );
  }
);

const IconButtonBinding15 = observer(function IconButtonBinding15({
  projectLabels,
  addingProject,
  addProject
}: {
  projectLabels: {
    unassigned: string;
    unavailable: string;
    actions: string;
    move: string;
    noWorkspaceProject: string;
    create: string;
    new: string;
    empty: string;
    loadFailed: string;
    folderLoadFailed: string;
    manage: string;
    remove: string;
    removed: string;
    restore: string;
    removedNotice: string;
    undo: string;
  };
  addingProject: boolean;
  addProject: () => Promise<void>;
}) {
  const onClickProp18 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => {
    void addProject();
  });
  return (
    <IconButton
      label={projectLabels.new}
      disabled={addingProject}
      aria-busy={addingProject}
      onClick={onClickProp18}
    >
      {addingProject ? <Loader2 className="animate-spin" /> : <FolderPlus />}
    </IconButton>
  );
});

const ButtonBinding18 = observer(function ButtonBinding18({
  providers,
  labels,
  ...slotProps
}: {
  providers:
    | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
        { id: LocalProviderId; name: string; available: boolean }[],
        Error
      >
    | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
        { id: LocalProviderId; name: string; available: boolean }[],
        Error
      >;
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    providers:
      | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
          { id: LocalProviderId; name: string; available: boolean }[],
          Error
        >
      | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
          { id: LocalProviderId; name: string; available: boolean }[],
          Error
        >;
    labels: {
      sessions: string;
      new: string;
      search: string;
      empty: string;
      noResults: string;
      retry: string;
      loading: string;
      archive: string;
      history: string;
      pinned: string;
      unpin: string;
      directory: string;
      newTitle: string;
      storage: string;
      directoryLocked: string;
      unavailable: string;
      failed: string;
      fork: string;
      forking: string;
      forkHint: string;
      forkWaiting: string;
      forkEmpty: string;
    };
  }
>) {
  const onClickProp22 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void providers.refetch();
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        variant="ghost"
        size="xs"
        disabled={providers.isFetching}
        onClick={onClickProp22}
      >
        {labels.retry}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding19 = observer(function ButtonBinding19({
  folderQuery,
  labels,
  ...slotProps
}: {
  folderQuery:
    | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
        LocalFolderProject[],
        Error
      >
    | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
        LocalFolderProject[],
        Error
      >;
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    folderQuery:
      | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
          LocalFolderProject[],
          Error
        >
      | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
          LocalFolderProject[],
          Error
        >;
    labels: {
      sessions: string;
      new: string;
      search: string;
      empty: string;
      noResults: string;
      retry: string;
      loading: string;
      archive: string;
      history: string;
      pinned: string;
      unpin: string;
      directory: string;
      newTitle: string;
      storage: string;
      directoryLocked: string;
      unavailable: string;
      failed: string;
      fork: string;
      forking: string;
      forkHint: string;
      forkWaiting: string;
      forkEmpty: string;
    };
  }
>) {
  const onClickProp23 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void folderQuery.refetch();
  });
  return (
    <Slot.Root {...slotProps}>
      <Button variant="ghost" size="xs" onClick={onClickProp23}>
        {labels.retry}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding20 = observer(function ButtonBinding20({
  sessions,
  labels,
  ...slotProps
}: {
  sessions: {
    data: LocalSessionEntry[];
    groups: import("../platform/local-conversation-pages").ConversationPageGroup[];
    loading: Set<string | null>;
    loadMore: (
      group: import("../platform/local-conversation-pages").ConversationPageGroup
    ) => Promise<void>;
    isPending: boolean;
    isError: boolean;
    error: Error | null;
    isFetching: boolean;
    refetch: () => void;
  };
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    sessions: {
      data: LocalSessionEntry[];
      groups: import("../platform/local-conversation-pages").ConversationPageGroup[];
      loading: Set<string | null>;
      loadMore: (
        group: import("../platform/local-conversation-pages").ConversationPageGroup
      ) => Promise<void>;
      isPending: boolean;
      isError: boolean;
      error: Error | null;
      isFetching: boolean;
      refetch: () => void;
    };
    labels: {
      sessions: string;
      new: string;
      search: string;
      empty: string;
      noResults: string;
      retry: string;
      loading: string;
      archive: string;
      history: string;
      pinned: string;
      unpin: string;
      directory: string;
      newTitle: string;
      storage: string;
      directoryLocked: string;
      unavailable: string;
      failed: string;
      fork: string;
      forking: string;
      forkHint: string;
      forkWaiting: string;
      forkEmpty: string;
    };
  }
>) {
  const onClickProp24 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void sessions.refetch();
  });
  return (
    <Slot.Root {...slotProps}>
      <Button variant="outline" onClick={onClickProp24}>
        {labels.retry}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding21 = observer(function ButtonBinding21({
  title,
  group,
  expanded,
  setCollapsedProjects,
  bindingKey: key,
  ...slotProps
}: {
  title: string;
  group: import("../platform/local-conversation-pages").ConversationPageGroup;
  expanded: boolean;
  setCollapsedProjects: import("react").Dispatch<
    import("react").SetStateAction<Set<string>>
  >;
  bindingKey: string;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    title: string;
    group: import("../platform/local-conversation-pages").ConversationPageGroup;
    expanded: boolean;
    setCollapsedProjects: import("react").Dispatch<
      import("react").SetStateAction<Set<string>>
    >;
    bindingKey: string;
  }
>) {
  const onClickProp25 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() =>
    setCollapsedProjects((previous) => {
      const next = new Set(previous);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    })
  );
  return (
    <Slot.Root {...slotProps}>
      <Button
        variant="ghost"
        size="sm"
        aria-label={title}
        title={group.project?.path}
        aria-expanded={expanded}
        className="min-w-0 flex-1 justify-start pr-0 text-label text-muted-foreground"
        onClick={onClickProp25}
      >
        {expanded ? (
          <ChevronDown className="size-3.5 shrink-0" />
        ) : (
          <ChevronRight className="size-3.5 shrink-0" />
        )}
        <FolderOpen className="size-3.5 shrink-0" />
        <span className="truncate">{title}</span>
      </Button>
    </Slot.Root>
  );
});

const IconButtonBinding22 = observer(function IconButtonBinding22({
  projectLabels,
  title,
  canCreate,
  createDisabledReason,
  create,
  group
}: {
  projectLabels: {
    unassigned: string;
    unavailable: string;
    actions: string;
    move: string;
    noWorkspaceProject: string;
    create: string;
    new: string;
    empty: string;
    loadFailed: string;
    folderLoadFailed: string;
    manage: string;
    remove: string;
    removed: string;
    restore: string;
    removedNotice: string;
    undo: string;
  };
  title: string;
  canCreate: boolean;
  createDisabledReason: string;
  create: (folderProjectId?: string | null) => void;
  group: import("../platform/local-conversation-pages").ConversationPageGroup;
}) {
  const onClickProp26 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => create(group.id));
  return (
    <IconButton
      size="icon-xs"
      label={`${projectLabels.create}：${title}`}
      disabled={!canCreate}
      disabledReason={createDisabledReason}
      onClick={onClickProp26}
    >
      <Plus className="size-3.5" />
    </IconButton>
  );
});

const DropdownMenuTriggerBinding23 = observer(
  function DropdownMenuTriggerBinding23({
    projectLabels,
    title
  }: {
    projectLabels: {
      unassigned: string;
      unavailable: string;
      actions: string;
      move: string;
      noWorkspaceProject: string;
      create: string;
      new: string;
      empty: string;
      loadFailed: string;
      folderLoadFailed: string;
      manage: string;
      remove: string;
      removed: string;
      restore: string;
      removedNotice: string;
      undo: string;
    };
    title: string;
  }) {
    const onPointerDownProp27 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof DropdownMenuTrigger>["onPointerDown"]
      >
    >((event) => event.preventBaseUIHandler());
    const onMouseDownProp28 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof DropdownMenuTrigger>["onMouseDown"]
      >
    >((event) => event.preventBaseUIHandler());
    return (
      <DropdownMenuTrigger
        // Activate on click; desktop accessibility presses may omit compatibility mouse events.
        onPointerDown={onPointerDownProp27}
        onMouseDown={onMouseDownProp28}
        render={
          <IconButton
            size="icon-xs"
            label={`${projectLabels.manage}：${title}`}
          />
        }
      >
        <MoreHorizontal className="size-3.5" />
      </DropdownMenuTrigger>
    );
  }
);

const DropdownMenuItemBinding24 = observer(function DropdownMenuItemBinding24({
  removeProject,
  bindingKey: key,
  projectLabels,
  ...slotProps
}: {
  removeProject: (projectId: string) => void;
  bindingKey: string;
  projectLabels: {
    unassigned: string;
    unavailable: string;
    actions: string;
    move: string;
    noWorkspaceProject: string;
    create: string;
    new: string;
    empty: string;
    loadFailed: string;
    folderLoadFailed: string;
    manage: string;
    remove: string;
    removed: string;
    restore: string;
    removedNotice: string;
    undo: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    removeProject: (projectId: string) => void;
    bindingKey: string;
    projectLabels: {
      unassigned: string;
      unavailable: string;
      actions: string;
      move: string;
      noWorkspaceProject: string;
      create: string;
      new: string;
      empty: string;
      loadFailed: string;
      folderLoadFailed: string;
      manage: string;
      remove: string;
      removed: string;
      restore: string;
      removedNotice: string;
      undo: string;
    };
  }
>) {
  const onClickProp29 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DropdownMenuItem>["onClick"]>
  >(() => removeProject(key));
  return (
    <Slot.Root {...slotProps}>
      <DropdownMenuItem onClick={onClickProp29}>
        <FolderMinus />
        {projectLabels.remove}
      </DropdownMenuItem>
    </Slot.Root>
  );
});

const ButtonBinding25 = observer(function ButtonBinding25({
  sessions,
  group,
  report,
  labels,
  chinese,
  ...slotProps
}: {
  sessions: {
    data: LocalSessionEntry[];
    groups: import("../platform/local-conversation-pages").ConversationPageGroup[];
    loading: Set<string | null>;
    loadMore: (
      group: import("../platform/local-conversation-pages").ConversationPageGroup
    ) => Promise<void>;
    isPending: boolean;
    isError: boolean;
    error: Error | null;
    isFetching: boolean;
    refetch: () => void;
  };
  group: import("../platform/local-conversation-pages").ConversationPageGroup;
  report: (error: unknown) => string | number;
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
  chinese: boolean;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    sessions: {
      data: LocalSessionEntry[];
      groups: import("../platform/local-conversation-pages").ConversationPageGroup[];
      loading: Set<string | null>;
      loadMore: (
        group: import("../platform/local-conversation-pages").ConversationPageGroup
      ) => Promise<void>;
      isPending: boolean;
      isError: boolean;
      error: Error | null;
      isFetching: boolean;
      refetch: () => void;
    };
    group: import("../platform/local-conversation-pages").ConversationPageGroup;
    report: (error: unknown) => string | number;
    labels: {
      sessions: string;
      new: string;
      search: string;
      empty: string;
      noResults: string;
      retry: string;
      loading: string;
      archive: string;
      history: string;
      pinned: string;
      unpin: string;
      directory: string;
      newTitle: string;
      storage: string;
      directoryLocked: string;
      unavailable: string;
      failed: string;
      fork: string;
      forking: string;
      forkHint: string;
      forkWaiting: string;
      forkEmpty: string;
    };
    chinese: boolean;
  }
>) {
  const onClickProp30 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void sessions.loadMore(group).catch(report);
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        variant="ghost"
        size="sm"
        className="mx-2 bg-muted/50 dark:bg-muted/25"
        disabled={sessions.loading.has(group.id) || sessions.isFetching}
        aria-busy={sessions.loading.has(group.id)}
        onClick={onClickProp30}
      >
        {sessions.loading.has(group.id)
          ? labels.loading
          : chinese
            ? "加载更多"
            : "Load more"}
      </Button>
    </Slot.Root>
  );
});

const DropdownMenuTriggerBinding26 = observer(
  function DropdownMenuTriggerBinding26({
    projectLabels,
    chinese,
    hiddenProjectIds
  }: {
    projectLabels: {
      unassigned: string;
      unavailable: string;
      actions: string;
      move: string;
      noWorkspaceProject: string;
      create: string;
      new: string;
      empty: string;
      loadFailed: string;
      folderLoadFailed: string;
      manage: string;
      remove: string;
      removed: string;
      restore: string;
      removedNotice: string;
      undo: string;
    };
    chinese: boolean;
    hiddenProjectIds: string[];
  }) {
    const onPointerDownProp31 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof DropdownMenuTrigger>["onPointerDown"]
      >
    >((event) => event.preventBaseUIHandler());
    const onMouseDownProp32 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof DropdownMenuTrigger>["onMouseDown"]
      >
    >((event) => event.preventBaseUIHandler());
    return (
      <DropdownMenuTrigger
        onPointerDown={onPointerDownProp31}
        onMouseDown={onMouseDownProp32}
        render={
          <Button
            variant="ghost"
            size="sm"
            className="text-caption text-muted-foreground"
          />
        }
      >
        {projectLabels.removed}
        {chinese
          ? `（${hiddenProjectIds.length}）`
          : ` (${hiddenProjectIds.length})`}
        <ChevronDown className="size-3.5" />
      </DropdownMenuTrigger>
    );
  }
);

const DropdownMenuItemBinding27 = observer(function DropdownMenuItemBinding27({
  id,
  projectLabels,
  title,
  restoreProject,
  ...slotProps
}: {
  id: string;
  projectLabels: {
    unassigned: string;
    unavailable: string;
    actions: string;
    move: string;
    noWorkspaceProject: string;
    create: string;
    new: string;
    empty: string;
    loadFailed: string;
    folderLoadFailed: string;
    manage: string;
    remove: string;
    removed: string;
    restore: string;
    removedNotice: string;
    undo: string;
  };
  title: string;
  restoreProject: (projectId: string) => void;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    id: string;
    projectLabels: {
      unassigned: string;
      unavailable: string;
      actions: string;
      move: string;
      noWorkspaceProject: string;
      create: string;
      new: string;
      empty: string;
      loadFailed: string;
      folderLoadFailed: string;
      manage: string;
      remove: string;
      removed: string;
      restore: string;
      removedNotice: string;
      undo: string;
    };
    title: string;
    restoreProject: (projectId: string) => void;
  }
>) {
  const onClickProp33 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof DropdownMenuItem>["onClick"]>
  >(() => restoreProject(id));
  return (
    <Slot.Root {...slotProps}>
      <DropdownMenuItem
        key={id}
        aria-label={`${projectLabels.restore}：${title}`}
        onClick={onClickProp33}
      >
        <FolderPlus />
        <span className="truncate">{title}</span>
      </DropdownMenuItem>
    </Slot.Root>
  );
});

const ResizableHandleBinding29 = observer(function ResizableHandleBinding29({
  compact,
  chinese,
  rememberSessionResize
}: {
  compact: boolean;
  chinese: boolean;
  rememberSessionResize: (endEvent: "pointerup" | "keyup") => void;
}) {
  const onPointerDownProp35 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof ResizableHandle>["onPointerDown"]>
  >((event) => {
    if (event.button === 0) rememberSessionResize("pointerup");
  });
  const onKeyDownProp36 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof ResizableHandle>["onKeyDown"]>
  >((event) => {
    if (["ArrowLeft", "ArrowRight", "Home", "End", "Enter"].includes(event.key))
      rememberSessionResize("keyup");
  });
  return (
    <ResizableHandle
      hidden={compact}
      aria-label={chinese ? "调整会话列表宽度" : "Resize conversation list"}
      onPointerDown={onPointerDownProp35}
      onKeyDown={onKeyDownProp36}
    />
  );
});

const IconButtonBinding30 = observer(function IconButtonBinding30({
  labels,
  setShowList
}: {
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
  setShowList: import("react").Dispatch<
    import("react").SetStateAction<boolean>
  >;
}) {
  const onClickProp37 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => setShowList(true));
  return (
    <IconButton label={labels.sessions} onClick={onClickProp37}>
      <ArrowLeft />
    </IconButton>
  );
});

const ButtonBinding31 = observer(function ButtonBinding31({
  labels,
  selected,
  automaticDirectory,
  directoryName,
  transport,
  scope,
  v2,
  report,
  ...slotProps
}: {
  labels: {
    sessions: string;
    new: string;
    search: string;
    empty: string;
    noResults: string;
    retry: string;
    loading: string;
    archive: string;
    history: string;
    pinned: string;
    unpin: string;
    directory: string;
    newTitle: string;
    storage: string;
    directoryLocked: string;
    unavailable: string;
    failed: string;
    fork: string;
    forking: string;
    forkHint: string;
    forkWaiting: string;
    forkEmpty: string;
  };
  selected: LocalSessionEntry;
  automaticDirectory: boolean;
  directoryName: string;
  transport: import("../platform/local-v2-transport").DirectLocalV2Transport;
  scope: { userId: string; workspaceId: string };
  v2: import("../../../../../../packages/conversation-client/services/conversation-service").ConversationService;
  report: (error: unknown) => string | number;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    labels: {
      sessions: string;
      new: string;
      search: string;
      empty: string;
      noResults: string;
      retry: string;
      loading: string;
      archive: string;
      history: string;
      pinned: string;
      unpin: string;
      directory: string;
      newTitle: string;
      storage: string;
      directoryLocked: string;
      unavailable: string;
      failed: string;
      fork: string;
      forking: string;
      forkHint: string;
      forkWaiting: string;
      forkEmpty: string;
    };
    selected: LocalSessionEntry;
    automaticDirectory: boolean;
    directoryName: string;
    transport: import("../platform/local-v2-transport").DirectLocalV2Transport;
    scope: { userId: string; workspaceId: string };
    v2: import("../../../../../../packages/conversation-client/services/conversation-service").ConversationService;
    report: (error: unknown) => string | number;
  }
>) {
  const onClickProp38 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void transport
      .choose(scope, {
        purpose: "conversation_directory",
        conversationId: selected.id,
        defaultPath: selected.cwd
      })
      .then((changed) => {
        if (changed) v2.receiveSummary(localV2SummarySchema.parse(changed));
      })
      .catch((error: unknown) => report(localV2Error(error)));
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        variant="ghost"
        size="sm"
        className="min-w-0 max-w-48 shrink"
        aria-label={labels.directory}
        disabled={selected.turnCount > 0}
        title={`${automaticDirectory ? directoryName : selected.cwd}${selected.turnCount ? ` · ${labels.directoryLocked}` : ""}`}
        onClick={onClickProp38}
      >
        <FolderOpen className="size-4" />
        <span className="truncate">{directoryName}</span>
      </Button>
    </Slot.Root>
  );
});

const IconButtonBinding32 = observer(function IconButtonBinding32({
  sideOpen,
  chinese,
  sideWorkspace
}: {
  sideOpen: boolean;
  chinese: boolean;
  sideWorkspace: import("react").RefObject<SideWorkspaceHandle | null>;
}) {
  const onClickProp39 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => sideWorkspace.current?.toggle());
  return (
    <IconButton
      aria-expanded={sideOpen}
      label={
        sideOpen
          ? chinese
            ? "关闭右侧面板"
            : "Close right workspace"
          : chinese
            ? "打开右侧面板"
            : "Open right workspace"
      }
      onClick={onClickProp39}
    >
      <PanelRightOpen />
    </IconButton>
  );
});

const TuttiAgentGuiPanelBinding33 = observer(
  function TuttiAgentGuiPanelBinding33({
    locateRequest,
    referenceDraft,
    workspace,
    userId,
    factory,
    slug,
    selectedId,
    provider,
    active,
    mainVisible,
    compact,
    showList,
    focusSequence,
    newConversationRequest,
    setReferenceDraft,
    openInNewTab,
    paths,
    push
  }: {
    locateRequest: {
      sessionId: string;
      turnId?: string;
      quote?: string;
      sequence: number;
    } | null;
    referenceDraft:
      | import("../../../../../../packages/views/chat/components/use-chat-draft-restore").RestoreDraftRequest
      | null;
    workspace: import("../../../../../../packages/core/types/workspace").Workspace;
    userId: string;
    factory: (
      options: ParentServiceOptions
    ) => import("../platform/agent-gui-parent-service").AgentGuiParentService;
    slug: string;
    selectedId: string | null;
    provider: LocalProviderId;
    active: boolean;
    mainVisible: boolean;
    compact: boolean;
    showList: boolean;
    focusSequence: number;
    newConversationRequest:
      | import("@multica/tutti-agent-gui-host/protocol").AgentGuiNewConversationRequest
      | undefined;
    setReferenceDraft: import("react").Dispatch<
      import("react").SetStateAction<
        | import("../../../../../../packages/views/chat/components/use-chat-draft-restore").RestoreDraftRequest
        | null
      >
    >;
    openInNewTab:
      | ((path: string, title?: string, opts?: { activate?: boolean }) => void)
      | undefined;
    paths: {
      root: () => string;
      usage: () => string;
      issues: () => string;
      issueDetail: (id: string) => string;
      projects: () => string;
      projectDetail: (id: string) => string;
      autopilots: () => string;
      autopilotDetail: (id: string) => string;
      agents: () => string;
      conversationFlow: () => string;
      conversationFlowDevice: (peerId: string) => string;
      newAgent: () => string;
      newAgentManual: () => string;
      newAgentDone: (agentId: string) => string;
      newAgentAi: () => string;
      newAgentAiSession: (sessionId: string) => string;
      agentDetail: (id: string) => string;
      agentConversationStarters: (id: string) => string;
      memberDetail: (id: string) => string;
      squads: () => string;
      squadDetail: (id: string) => string;
      inbox: () => string;
      chat: () => string;
      chatWithAgent: (agentId: string) => string;
      chatSession: (sessionId: string) => string;
      myIssues: () => string;
      runtimes: () => string;
      runtimeDetail: (id: string) => string;
      runtimeSettings: (machineId: string, runtimeId: string) => string;
      skills: () => string;
      skillDetail: (id: string) => string;
      settings: () => string;
      attachmentPreview: (id: string) => string;
    };
    push: (path: string) => void;
  }) {
    const device = useLocalDevice();
    const onRestoreDraftAppliedProp40 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof TuttiAgentGuiPanel>["onRestoreDraftApplied"]
      >
    >((id) => {
      setReferenceDraft(null);
      if (selectedId) useIssueConversationStore.getState().applied(
        issueConversationDraftKey({ userId, workspaceId: workspace.id }, device.id, selectedId), id,
      );
    });
    const onOpenTaskProp41 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof TuttiAgentGuiPanel>["onOpenTask"]>
    >((id) => {
      if (openInNewTab)
        openInNewTab(paths.issueDetail(id), undefined, { activate: true });
      else push(paths.issueDetail(id));
    });
    const onShowMessageProp42 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof TuttiAgentGuiPanel>["onShowMessage"]
      >
    >((message, tone) => toast[tone ?? "info"](message));
    return (
      <TuttiAgentGuiPanel
        locateRequest={locateRequest ?? undefined}
        restoreDraftRequest={referenceDraft}
        key={`${workspace.id}:${userId}`}
        serviceFactory={factory}
        workspaceId={workspace.id}
        workspaceSlug={slug}
        userId={userId}
        activeSessionId={selectedId}
        selectedAgentId={localTargetId(provider)}
        active={active && mainVisible && (!compact || !showList)}
        composerFocusSequence={focusSequence || undefined}
        newConversationRequest={newConversationRequest}
        onRestoreDraftApplied={onRestoreDraftAppliedProp40}
        onOpenTask={onOpenTaskProp41}
        onShowMessage={onShowMessageProp42}
      />
    );
  }
);

const FormBinding2 = observer(function FormBinding2({
  rename,
  renameLabels,
  renameInput,
  renameDraft,
  updatingId,
  renameError,
  renameErrorId,
  setRenameDraft,
  setRenameError,
  setRenameTarget
}: {
  rename: () => Promise<void>;
  renameLabels: {
    action: string;
    title: string;
    name: string;
    save: string;
    saving: string;
    cancel: string;
  };
  renameInput: import("react").RefObject<HTMLInputElement | null>;
  renameDraft: string;
  updatingId: string | null;
  renameError: string | null;
  renameErrorId: string;
  setRenameDraft: import("react").Dispatch<
    import("react").SetStateAction<string>
  >;
  setRenameError: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
  setRenameTarget: import("react").Dispatch<
    import("react").SetStateAction<Pick<
      LocalSessionEntry,
      "id" | "title"
    > | null>
  >;
}) {
  const onSubmitProp44 = usePersistCallback<
    NonNullable<React.ComponentProps<"form">["onSubmit"]>
  >((event) => {
    event.preventDefault();
    void rename();
  });
  return (
    <form className="grid gap-4" onSubmit={onSubmitProp44}>
      <DialogHeader>
        <DialogTitle>{renameLabels.title}</DialogTitle>
      </DialogHeader>
      <label className="grid gap-2 text-label">
        {renameLabels.name}
        <InputBinding12
          renameInput={renameInput}
          renameDraft={renameDraft}
          updatingId={updatingId}
          renameError={renameError}
          renameErrorId={renameErrorId}
          setRenameDraft={setRenameDraft}
          setRenameError={setRenameError}
        />
      </label>
      {renameError && (
        <p
          id={renameErrorId}
          role="alert"
          className="text-caption text-destructive"
        >
          {renameError}
        </p>
      )}
      <DialogFooter>
        <ButtonBinding13
          updatingId={updatingId}
          setRenameTarget={setRenameTarget}
          renameLabels={renameLabels}
        />
        <Button
          type="submit"
          disabled={updatingId !== null || !renameDraft.trim()}
          aria-busy={updatingId !== null}
        >
          {updatingId !== null && <Loader2 className="animate-spin" />}
          {updatingId !== null ? renameLabels.saving : renameLabels.save}
        </Button>
      </DialogFooter>
    </form>
  );
});

const LocalSideWorkspaceBinding3 = observer(
  function LocalSideWorkspaceBinding3({
    workspace,
    userId,
    sideWorkspace,
    setSideOpen,
    selectedId,
    scope,
    slug,
    selected,
    provider,
    sessions,
    chinese,
    active,
    compact,
    showList,
    latest,
    setReferenceDraft,
    openInNewTab,
    paths,
    push,
    labels,
    setShowList,
    automaticDirectory,
    directoryName,
    transport,
    v2,
    report,
    sideOpen,
    remote,
    locateRequest,
    referenceDraft,
    factory,
    focusSequence,
    newConversationRequest
  }: {
    workspace: import("../../../../../../packages/core/types/workspace").Workspace;
    userId: string;
    sideWorkspace: import("react").RefObject<SideWorkspaceHandle | null>;
    setSideOpen: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    selectedId: string | null;
    scope: { userId: string; workspaceId: string };
    slug: string;
    selected: LocalSessionEntry | undefined;
    provider: LocalProviderId;
    sessions: {
      data: LocalSessionEntry[];
      groups: import("../platform/local-conversation-pages").ConversationPageGroup[];
      loading: Set<string | null>;
      loadMore: (
        group: import("../platform/local-conversation-pages").ConversationPageGroup
      ) => Promise<void>;
      isPending: boolean;
      isError: boolean;
      error: Error | null;
      isFetching: boolean;
      refetch: () => void;
    };
    chinese: boolean;
    active: boolean;
    compact: boolean;
    showList: boolean;
    latest: import("react").RefObject<{
      select: (_id: string | null, _provider?: LocalProviderId) => void;
      locate: (_id: string, _turnId?: string, _quote?: string) => void;
    }>;
    setReferenceDraft: import("react").Dispatch<
      import("react").SetStateAction<
        | import("../../../../../../packages/views/chat/components/use-chat-draft-restore").RestoreDraftRequest
        | null
      >
    >;
    openInNewTab:
      | ((path: string, title?: string, opts?: { activate?: boolean }) => void)
      | undefined;
    paths: {
      root: () => string;
      usage: () => string;
      issues: () => string;
      issueDetail: (id: string) => string;
      projects: () => string;
      projectDetail: (id: string) => string;
      autopilots: () => string;
      autopilotDetail: (id: string) => string;
      agents: () => string;
      conversationFlow: () => string;
      conversationFlowDevice: (peerId: string) => string;
      newAgent: () => string;
      newAgentManual: () => string;
      newAgentDone: (agentId: string) => string;
      newAgentAi: () => string;
      newAgentAiSession: (sessionId: string) => string;
      agentDetail: (id: string) => string;
      agentConversationStarters: (id: string) => string;
      memberDetail: (id: string) => string;
      squads: () => string;
      squadDetail: (id: string) => string;
      inbox: () => string;
      chat: () => string;
      chatWithAgent: (agentId: string) => string;
      chatSession: (sessionId: string) => string;
      myIssues: () => string;
      runtimes: () => string;
      runtimeDetail: (id: string) => string;
      runtimeSettings: (machineId: string, runtimeId: string) => string;
      skills: () => string;
      skillDetail: (id: string) => string;
      settings: () => string;
      attachmentPreview: (id: string) => string;
    };
    push: (path: string) => void;
    labels: {
      sessions: string;
      new: string;
      search: string;
      empty: string;
      noResults: string;
      retry: string;
      loading: string;
      archive: string;
      history: string;
      pinned: string;
      unpin: string;
      directory: string;
      newTitle: string;
      storage: string;
      directoryLocked: string;
      unavailable: string;
      failed: string;
      fork: string;
      forking: string;
      forkHint: string;
      forkWaiting: string;
      forkEmpty: string;
    };
    setShowList: import("react").Dispatch<
      import("react").SetStateAction<boolean>
    >;
    automaticDirectory: boolean;
    directoryName: string;
    transport: import("../platform/local-v2-transport").DirectLocalV2Transport;
    v2: import("../../../../../../packages/conversation-client/services/conversation-service").ConversationService;
    report: (error: unknown) => string | number;
    sideOpen: boolean;
    remote: boolean;
    locateRequest: {
      sessionId: string;
      turnId?: string;
      quote?: string;
      sequence: number;
    } | null;
    referenceDraft:
      | import("../../../../../../packages/views/chat/components/use-chat-draft-restore").RestoreDraftRequest
      | null;
    factory: (
      options: ParentServiceOptions
    ) => import("../platform/agent-gui-parent-service").AgentGuiParentService;
    focusSequence: number;
    newConversationRequest:
      | import("@multica/tutti-agent-gui-host/protocol").AgentGuiNewConversationRequest
      | undefined;
  }) {
    const sessionsProp45 = useMemo<
      NonNullable<React.ComponentProps<typeof LocalSideWorkspace>["sessions"]>
    >(() => [], []);
    const onSelectSourceProp46 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof LocalSideWorkspace>["onSelectSource"]
      >
    >((sessionId, turnId, quote) =>
      latest.current.locate(sessionId, turnId, quote)
    );
    const onReferenceProp47 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof LocalSideWorkspace>["onReference"]
      >
    >((content) =>
      setReferenceDraft({
        id: crypto.randomUUID(),
        content,
        sessionId: selectedId ?? undefined
      })
    );
    const onOpenTaskProp48 = usePersistCallback<
      NonNullable<React.ComponentProps<typeof LocalSideWorkspace>["onOpenTask"]>
    >((id) => {
      if (openInNewTab)
        openInNewTab(paths.issueDetail(id), undefined, { activate: true });
      else push(paths.issueDetail(id));
    });
    const onShowMessageProp49 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof LocalSideWorkspace>["onShowMessage"]
      >
    >((message, tone) => toast[tone ?? "info"](message));
    return (
      <LocalSideWorkspace
        key={`${workspace.id}:${userId}`}
        ref={sideWorkspace}
        onOpenChange={setSideOpen}
        sessionId={selectedId}
        scope={scope}
        slug={slug}
        source={selected}
        provider={provider}
        sessions={sessions.data ?? sessionsProp45}
        chinese={chinese}
        active={active && (!compact || !showList)}
        onSelectSource={onSelectSourceProp46}
        onReference={onReferenceProp47}
        onOpenTask={onOpenTaskProp48}
        onShowMessage={onShowMessageProp49}
      >
        {(mainVisible) => (
          <div className="flex h-full min-h-0 min-w-0 flex-col">
            <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
              {compact && (
                <IconButtonBinding30
                  labels={labels}
                  setShowList={setShowList}
                />
              )}
              <span className="min-w-0 flex-1 truncate text-body font-medium">
                {selected?.title ?? labels.newTitle}
              </span>
              <div className="flex min-w-0 items-center gap-1">
                {selected && (
                  <ButtonBinding31
                    labels={labels}
                    selected={selected}
                    automaticDirectory={automaticDirectory}
                    directoryName={directoryName}
                    transport={transport}
                    scope={scope}
                    v2={v2}
                    report={report}
                  />
                )}
                <IconButtonBinding32
                  sideOpen={sideOpen}
                  chinese={chinese}
                  sideWorkspace={sideWorkspace}
                />
              </div>
            </div>
            <LocalRuntimeNotice scope={scope} chinese={chinese} />
            {remote && <RemoteFolderPicker chinese={chinese} />}
            <TuttiAgentGuiPanelBinding33
              key={`${workspace.id}:${userId}`}
              locateRequest={locateRequest}
              referenceDraft={referenceDraft}
              workspace={workspace}
              userId={userId}
              factory={factory}
              slug={slug}
              selectedId={selectedId}
              provider={provider}
              active={active}
              mainVisible={mainVisible}
              compact={compact}
              showList={showList}
              focusSequence={focusSequence}
              newConversationRequest={newConversationRequest}
              setReferenceDraft={setReferenceDraft}
              openInNewTab={openInNewTab}
              paths={paths}
              push={push}
            />
          </div>
        )}
      </LocalSideWorkspace>
    );
  }
);

const DialogBinding1 = observer(function DialogBinding1({
  renameTarget,
  updatingId,
  setRenameTarget,
  renameInput,
  renameReturnFocus,
  rename,
  renameLabels,
  renameDraft,
  renameError,
  renameErrorId,
  setRenameDraft,
  setRenameError
}: {
  renameTarget: Pick<LocalSessionEntry, "id" | "title"> | null;
  updatingId: string | null;
  setRenameTarget: import("react").Dispatch<
    import("react").SetStateAction<Pick<
      LocalSessionEntry,
      "id" | "title"
    > | null>
  >;
  renameInput: import("react").RefObject<HTMLInputElement | null>;
  renameReturnFocus: import("react").RefObject<HTMLAnchorElement | null>;
  rename: () => Promise<void>;
  renameLabels: {
    action: string;
    title: string;
    name: string;
    save: string;
    saving: string;
    cancel: string;
  };
  renameDraft: string;
  renameError: string | null;
  renameErrorId: string;
  setRenameDraft: import("react").Dispatch<
    import("react").SetStateAction<string>
  >;
  setRenameError: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
}) {
  const onOpenChangeProp50 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Dialog>["onOpenChange"]>
  >((open) => {
    if (!open && updatingId === null) setRenameTarget(null);
  });
  return (
    <Dialog open={renameTarget !== null} onOpenChange={onOpenChangeProp50}>
      <DialogContent
        initialFocus={renameInput}
        finalFocus={renameReturnFocus}
        showCloseButton={updatingId === null}
      >
        <FormBinding2
          rename={rename}
          renameLabels={renameLabels}
          renameInput={renameInput}
          renameDraft={renameDraft}
          updatingId={updatingId}
          renameError={renameError}
          renameErrorId={renameErrorId}
          setRenameDraft={setRenameDraft}
          setRenameError={setRenameError}
          setRenameTarget={setRenameTarget}
        />
      </DialogContent>
    </Dialog>
  );
});
