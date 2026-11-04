import { sideLayoutKey, sideSnapshot, readSideLayout as readLayout, prepareSideTab, applySideCommand, registerSideWorkspaceView, syncSideWorkspace, type SideTab as Tab, type SideLayout as Layout } from "./local-side-workspace-sync";
import { sideWorkspaceControlSchema } from "../../../shared/side-workspace";
import { SideActivityIcon, SideActivitySummary, useSideActivity } from "./local-side-feedback";
import { LocalRuntimeNotice } from "./local-runtime-notice";
import { conversationService } from "../platform/conversation-services";
import { localV2Preview } from "../platform/local-v2-preview";
import { sessionEntry, type LocalSessionEntry } from "../platform/local-conversation-projects";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { ConversationModel } from "@multica/conversation-client/models";
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState, useSyncExternalStore, type ComponentProps, type ReactNode, type SetStateAction } from "react";
import { useQueries } from "@tanstack/react-query";
import { ArrowLeft, File, FolderOpen, Globe, Loader2, Maximize2, MessagesSquare, Minimize2, PanelRightOpen, Plus, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator } from "@multica/ui/components/ui/dropdown-menu";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle, useDefaultLayout } from "@multica/ui/components/ui/resizable";
import { TuttiAgentGuiPanel } from "../components/tutti-agent-gui-panel";
import type { ParentServiceOptions } from "../platform/agent-gui-parent-service";
import type { LocalConversationScope, LocalProviderId } from "@multica/core/local-conversation/protocol";
import { FileLinkPreviewError, fileLinkErrorMessage, resolutionSource, resolveConversationFile } from "./local-resource-resolution";
import { createLocalConversationParentService, localConversationKey, localTargetId } from "../platform/local-conversation-service";
import { LocalFilePreview, PreviewFolder, WebResourcePreview } from "./local-resource-preview";
import { useLocalDevice } from "../platform/local-device";

export interface SideWorkspaceHandle { openSide(id: string, sourceId?: string): void; openResource(sessionId: string, url: string, sourceId?: string): Promise<void>; toggle(): void }
interface Props {
  scope: LocalConversationScope; slug: string; sessionId?: string | null; source?: LocalSessionEntry; provider: LocalProviderId; sessions: LocalSessionEntry[];
  chinese: boolean; active: boolean; onSelectSource(id: string, turnId?: string, quote?: string): void;
  onShowMessage(message: string, tone?: "success" | "info" | "warning" | "error"): void;
  onOpenTask(id: string): void; onReference(content: string): void;
  children: (visible: boolean) => ReactNode;
  onOpenChange?: (open: boolean) => void;
}
const emptyLayout = (): Layout => ({ tabs: [], selected: null, open: false });
function layoutKey(scope: LocalConversationScope, sessionId: string, deviceId: string) {
  return sideLayoutKey(scope, sessionId, deviceId);
}

export const LocalSideWorkspace = forwardRef<SideWorkspaceHandle, Props>(function LocalSideWorkspace(props, ref) {
  const device = useLocalDevice();
  const { transport } = device;
  const sessionId = props.sessionId === undefined ? props.source?.id : props.sessionId;
  const key = sessionId ? layoutKey(props.scope, sessionId, device.id) : "new";
  const [layouts, setLayouts] = useState<Record<string, Layout>>(() => ({ [key]: sessionId ? readLayout(key) : emptyLayout() }));
  const layoutsRef = useRef(layouts); layoutsRef.current = layouts;
  const layout = layouts[key] ?? (sessionId ? readLayout(key) : emptyLayout());
  // Sides the list has not loaded yet are read once; reading records their summary.
  const v2 = conversationService(transport, props.scope);
  const known = useSyncExternalStore(useCallback((listener: () => void) => v2.onSummaries(listener), [v2]), () => v2.summaries());
  const listed = new Set(props.sessions.map((entry) => entry.id));
  const extra = known.filter((summary) => !listed.has(summary.conversation_id) && layout.tabs.some((tab) => tab.kind === "side" && tab.sessionId === summary.conversation_id)).map(sessionEntry);
  useQueries({ queries: layout.tabs.filter((tab) => tab.kind === "side").map((tab) => ({
    queryKey: [...localConversationKey(props.scope, device.id), "side-summary", tab.sessionId],
    queryFn: async () => { await v2.snapshot(tab.sessionId); return true; },
    enabled: !listed.has(tab.sessionId) && !v2.summary(tab.sessionId),
  })) });
  const sessions = [...props.sessions, ...extra];
  const updateLayout = useCallback((owner: string, update: SetStateAction<Layout>) => {
    const current = layoutsRef.current;
    const previous = current[owner] ?? (owner === "new" ? emptyLayout() : readLayout(owner));
    const next = { ...current, [owner]: typeof update === "function" ? update(previous) : update };
    layoutsRef.current = next; setLayouts(next);
  }, []);
  const setLayout = useCallback((update: SetStateAction<Layout>) => updateLayout(key, update), [key, updateLayout]);
  const [maximized, setMaximized] = useState(false);
  const [narrow, setNarrow] = useState(false);
  const [addressOpen, setAddressOpen] = useState(false), [address, setAddress] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const [openingFiles, setOpeningFiles] = useState(0);
  const resourceRequests = useRef(new Map<string, Promise<void>>());
  const [owner, setOwner] = useState(key);
  const epoch = useRef(0);
  if (owner !== key) {
    // Restore before commit so the outgoing layout can never render under the new owner.
    setOwner(key);
    epoch.current += 1;
    setMaximized(false); setAddressOpen(false); setAddress(""); setCreating(false); setError(""); setOpeningFiles(0);
    setLayouts((current) => {
      const next = { ...current, [key]: current[key] ?? (sessionId ? readLayout(key) : emptyLayout()) };
      if (key !== "new") delete next.new;
      return next;
    });
  }
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const root = useRef<HTMLDivElement>(null);
  const latest = useRef(props); latest.current = props;
  const [browserActivity, setBrowserActivity] = useState<Record<string, { conversationId: string; turnId: string }>>({});
  const sync = useCallback(async (owner: string, value: Layout, visible: boolean) => {
    const prefix = sideLayoutKey(latest.current.scope, "", device.id);
    if (!owner.startsWith(prefix)) return;
    const snapshot = sideSnapshot(owner.slice(prefix.length), value, visible);
    await syncSideWorkspace(transport, latest.current.scope, snapshot);
    return snapshot;
  }, [transport, device.id]);
  useEffect(() => {
    const timer = setTimeout(() => {
      const values = { ...layoutsRef.current };
      for (const [owner, value] of Object.entries(values)) if (owner !== "new") void sync(owner, value, owner === key && props.active).catch(() => {});
    }, 150);
    return () => clearTimeout(timer);
  }, [layouts, key, props.active, sync]);
  useEffect(() => {
    const fullSync = () => {
      const prefix = sideLayoutKey(latest.current.scope, "", device.id);
      for (let index = 0; index < localStorage.length; index++) {
        const owner = localStorage.key(index);
        if (owner?.startsWith(prefix)) void sync(owner, layoutsRef.current[owner] ?? readLayout(owner), false).catch(() => {});
      }
      if (key !== "new") void sync(key, layoutsRef.current[key] ?? readLayout(key), latest.current.active).catch(() => {});
    };
    fullSync();
    // A restarted daemon has lost the layouts.
    return transport.onConnection((scope, connected) => { if (connected && scope.userId === latest.current.scope.userId && scope.workspaceId === latest.current.scope.workspaceId) fullSync(); });
  }, [props.scope.userId, props.scope.workspaceId, key, sync, transport, device.id]);
  useEffect(() => registerSideWorkspaceView({ userId: props.scope.userId, workspaceId: props.scope.workspaceId }, device.id), [props.scope.userId, props.scope.workspaceId, device.id]);
  useEffect(() => () => { if (key !== "new") void sync(key, layoutsRef.current[key] ?? readLayout(key), false).catch(() => {}); }, [key, sync]);
  useEffect(() => window.desktopAPI.sideWorkspace?.onActivity((value) => setBrowserActivity((current) => {
    const next = { ...current }; if (value.active && value.turnId) next[value.tabId] = { conversationId: value.conversationId, turnId: value.turnId }; else delete next[value.tabId]; return next;
  })), []);
  useEffect(() => window.desktopAPI.sideWorkspace?.onCommand((command) => {
    const request = sideWorkspaceControlSchema.parse(command.request), current = latest.current;
    if (request.scope.userId !== current.scope.userId || request.scope.workspaceId !== current.scope.workspaceId || (request.deviceId ?? "local") !== device.id) return;
    void (async () => {
      const owner = sideLayoutKey(current.scope, request.conversationId, device.id);
      const opened = await prepareSideTab(request, transport);
      if (!mounted.current || latest.current.scope.userId !== current.scope.userId || latest.current.scope.workspaceId !== current.scope.workspaceId) throw new Error("Side workspace changed");
      const previous = layoutsRef.current[owner] ?? readLayout(owner);
      const { layout: next, tabId } = applySideCommand(previous, request, opened);
      layoutsRef.current = { ...layoutsRef.current, [owner]: next }; setLayouts(layoutsRef.current);
      localStorage.setItem(owner, JSON.stringify(next));
      if (request.op === "select" || (request.op === "open" && request.select !== false)) current.onSelectSource(request.conversationId);
      const snapshot = await sync(owner, next, latest.current.active);
      await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
      await window.desktopAPI.sideWorkspace.ack({ id: command.id, snapshot, tabId });
    })().catch((error: unknown) => { void window.desktopAPI.sideWorkspace.ack({ id: command.id, error: error instanceof Error ? error.message : String(error) }); });
  }), [sync, transport, device.id]);
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({ id: "multica-side-workspace-width" });
  const onOpenChange = props.onOpenChange;
  useEffect(() => { onOpenChange?.(layout.open); }, [layout.open, onOpenChange]);
  useEffect(() => {
    for (const [owner, value] of Object.entries(layouts)) {
      if (owner === "new") continue;
      try { localStorage.setItem(owner, JSON.stringify(value)); } catch { /* Keep this open workspace usable when preference storage is full. */ }
    }
  }, [layouts]);
  useEffect(() => {
    if (layout.open) root.current?.querySelector('[role="tab"][aria-selected="true"]')?.parentElement?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [layout.selected, layout.open]);
  useEffect(() => { const element = root.current; if (!element) return; const observer = new ResizeObserver(([entry]) => setNarrow(entry!.contentRect.width < 760)); observer.observe(element); return () => observer.disconnect(); }, []);
  const currentKey = useRef(key); currentKey.current = key;
  const addTo = useCallback((owner: string, tab: Tab) => {
    const saved = layoutsRef.current[owner] ?? readLayout(owner);
    if (saved.tabs.length >= 20 && !saved.tabs.some((item) => item.id === tab.id || (item.kind === "web" && tab.kind === "web" && item.url === tab.url) || (item.kind === "file" && tab.kind === "file" && item.location.path === tab.location.path))) {
      latest.current.onShowMessage(latest.current.chinese ? "右侧工作区最多支持 20 个标签，请先关闭部分标签。" : "The Side workspace supports 20 tabs. Close a tab before opening another.", "warning"); return;
    }
    updateLayout(owner, (current) => {
    const existing = current.tabs.find((item) => item.id === tab.id || (item.kind === "web" && tab.kind === "web" && item.url === tab.url) || (item.kind === "file" && tab.kind === "file" && (item.selected ?? item.location).path === (tab.selected ?? tab.location).path));
    if (!existing && current.tabs.length >= 20) return current;
    return { tabs: existing ? current.tabs.map((item) => item.id === existing.id && item.kind === "file" && tab.kind === "file" ? { ...item, line: tab.line } : item) : [...current.tabs, tab], selected: existing?.id ?? tab.id, open: true };
  }); if (currentKey.current === owner) setError(""); }, [updateLayout]);
  const add = useCallback((tab: Tab) => addTo(key, tab), [addTo, key]);
  const openSide = useCallback((id: string, sourceId?: string) => addTo(sourceId ? layoutKey(props.scope, sourceId, device.id) : key, { id: `side:${id}`, kind: "side", sessionId: id }), [addTo, key, props.scope, device.id]);
  const openResourceFor = useCallback((owner: string, sessionId: string, value: string) => {
    const requestKey = JSON.stringify([owner, sessionId, value]);
    const pending = resourceRequests.current.get(requestKey);
    if (pending) return pending;
    const started = epoch.current;
    const isCurrent = () => mounted.current && currentKey.current === owner && epoch.current === started;
    const operation = (async () => {
      if (/^https?:\/\//i.test(value)) { const url = new URL(value); if (url.username || url.password) throw new Error("Use a webpage address without credentials"); addTo(owner, { id: `web:${crypto.randomUUID()}`, kind: "web", url: url.href }); return; }
      const scope = latest.current.scope;
      if (isCurrent()) { setOpeningFiles((count) => count + 1); setError(""); }
      updateLayout(owner, (current) => ({ ...current, open: true }));
      try {
        const source = resolutionSource(ConversationModel.fromSnapshot(await conversationService(transport, scope).snapshot(sessionId)));
        const preview = localV2Preview(transport, scope);
        const { file, line } = await resolveConversationFile({ source, value,
          read: async (path) => preview.read(await preview.locate(sessionId, path), sessionId),
          list: (path) => preview.list({ root: source.cwd, path }, sessionId),
        });
        if (!mounted.current || (owner === "new" && !isCurrent())) return;
        addTo(owner, { id: `file:${crypto.randomUUID()}`, kind: "file", sourceId: sessionId, line, location: { root: file.root ?? source.cwd, path: file.path, name: file.name, kind: "file" } });
      } catch (error) {
        if (!(error instanceof FileLinkPreviewError)) throw error;
        if (isCurrent()) setError(fileLinkErrorMessage(error, latest.current.chinese));
      } finally { if (isCurrent()) setOpeningFiles((count) => Math.max(0, count - 1)); }
    })();
    resourceRequests.current.set(requestKey, operation);
    void operation.finally(() => resourceRequests.current.delete(requestKey)).catch(() => {});
    return operation;
  }, [addTo, updateLayout, transport]);
  const openResource = useCallback((sessionId: string, value: string, sourceId?: string) => openResourceFor(sourceId ? layoutKey(props.scope, sourceId, device.id) : key, sessionId, value), [key, openResourceFor, props.scope, device.id]);
  useImperativeHandle(ref, () => ({ openSide, openResource, toggle: () => setLayout((current) => ({ ...current, open: !current.open })) }), [openSide, openResource, setLayout]);
  const showError = (error: unknown) => { const message = error instanceof Error ? error.message : String(error); setError(message); props.onShowMessage(message, "error"); };
  const createSide = async () => {
    if (creating) return; setCreating(true);
    const started = epoch.current;
    const isCurrent = () => mounted.current && epoch.current === started;
    try {
      const client = conversationService(transport, props.scope);
      let sourceId = props.source?.id ?? sessionId;
      if (!sourceId) {
        const created = localV2SummarySchema.parse(await client.request("POST", "/sessions", { id: crypto.randomUUID(), provider: props.provider, title: "" }));
        client.receiveSummary(created); sourceId = created.conversation_id;
      }
      const summary = localV2SummarySchema.parse(await client.command(sourceId, { type: "create_side", request_id: crypto.randomUUID() }));
      client.receiveSummary(summary);
      const side = { id: summary.conversation_id }, source = { id: sourceId };
      if (!mounted.current) return;
      if (sessionId) openSide(side.id);
      else {
        const tab: Tab = { id: `side:${side.id}`, kind: "side", sessionId: side.id };
        updateLayout(layoutKey(props.scope, source.id, device.id), (current) => ({ tabs: [...current.tabs, tab], selected: tab.id, open: true }));
        if (isCurrent()) props.onSelectSource(source.id);
      }
    } catch (error) { if (isCurrent()) showError(error); } finally { if (isCurrent()) setCreating(false); }
  };
  const chooseFile = async (kind: "file" | "directory") => {
    const started = epoch.current;
    try {
      const location = await localV2Preview(transport, props.scope).choose(kind);
      if (location && mounted.current && (key !== "new" || epoch.current === started)) add({ id: `file:${crypto.randomUUID()}`, kind: "file", location });
    } catch (error) { if (mounted.current && epoch.current === started) showError(error); }
  };
  const close = (id: string) => {
    const current = layoutsRef.current[key] ?? layout, tabs = current.tabs.filter((tab) => tab.id !== id);
    const next = { tabs, selected: current.selected === id ? tabs.at(-1)?.id ?? null : current.selected, open: tabs.length > 0 && current.open };
    setLayout(next); if (key !== "new") void sync(key, next, props.active).catch(() => {});
  };
  const sideVisible = layout.open, mainVisible = !(sideVisible && (maximized || narrow));
  const title = (tab: Tab) => tab.kind === "side" ? `${props.chinese ? "旁支" : "Side"} · ${sessions.find((entry) => entry.id === tab.sessionId)?.title.replace(/ · Side$/, "") ?? (props.chinese ? "新对话" : "New chat")}` : tab.kind === "web" ? tab.title || new URL(tab.url).hostname : (tab.selected ?? tab.location).name;
  const activeTab = layout.tabs.find((tab) => tab.id === layout.selected);
  return <div ref={root} className="flex h-full min-h-0 min-w-0 flex-col [&_[data-panel][hidden]]:hidden!" data-testid="local-side-workspace">
    <ResizablePanelGroup orientation="horizontal" disabled={!sideVisible || !mainVisible} defaultLayout={defaultLayout} onLayoutChanged={onLayoutChanged}>
      <ResizablePanel id="side-main" minSize="32%" hidden={!mainVisible}><div className="h-full min-w-0">{props.children(mainVisible)}</div></ResizablePanel>
      {sideVisible && mainVisible && <ResizableHandle aria-label={props.chinese ? "调整右侧面板宽度" : "Resize right workspace"} onPointerDown={() => { document.documentElement.dataset.sideDragging = "true"; const end = () => { delete document.documentElement.dataset.sideDragging; window.removeEventListener("pointerup", end); window.removeEventListener("pointercancel", end); window.removeEventListener("blur", end); }; window.addEventListener("pointerup", end, { once: true }); window.addEventListener("pointercancel", end, { once: true }); window.addEventListener("blur", end, { once: true }); }} />}
      <ResizablePanel id="side-resources" defaultSize="46%" minSize="30%" hidden={!sideVisible}>
        <div className="flex h-full min-h-0 min-w-0 flex-col border-l bg-background" aria-label={props.chinese ? "右侧工作区" : "Right workspace"}>
          <div className="flex h-12 shrink-0 items-center gap-1 border-b px-2">
            {!mainVisible && <IconButton label={props.chinese ? "返回主对话" : "Return to main chat"} onClick={() => { setMaximized(false); if (narrow) setLayout((current) => ({ ...current, open: false })); }}><ArrowLeft /></IconButton>}
            <div role="tablist" aria-label={props.chinese ? "右侧内容" : "Workspace tabs"} className="flex min-w-0 flex-1 overflow-x-auto">
              {layout.tabs.map((tab) => <div key={tab.id} className={`flex min-w-24 max-w-48 shrink-0 items-center rounded-t-md border-b-2 ${tab.id === layout.selected ? "border-foreground bg-surface-selected" : "border-transparent"}`}>
                <WorkspaceTab tab={tab} chinese={props.chinese} scope={props.scope} session={tab.kind === "side" ? sessions.find((entry) => entry.id === tab.sessionId) : undefined} active={props.active && sideVisible} title={title(tab)} role="tab" id={`resource-tab-${tab.id}`} aria-selected={tab.id === layout.selected} aria-controls={`resource-content-${tab.id}`} tabIndex={tab.id === layout.selected ? 0 : -1} variant="ghost" size="sm" className="min-w-0 flex-1 justify-start rounded-none px-2 text-label" onClick={() => setLayout((current) => ({ ...current, selected: tab.id }))} onKeyDown={(event) => {
                  const index = layout.tabs.findIndex((item) => item.id === tab.id); const next = event.key === "ArrowRight" ? layout.tabs[(index + 1) % layout.tabs.length] : event.key === "ArrowLeft" ? layout.tabs[(index + layout.tabs.length - 1) % layout.tabs.length] : event.key === "Home" ? layout.tabs[0] : event.key === "End" ? layout.tabs.at(-1) : undefined;
                  if (next) { event.preventDefault(); setLayout((current) => ({ ...current, selected: next.id })); document.getElementById(`resource-tab-${next.id}`)?.focus(); }
                }} />
                {browserActivity[tab.id] && <span aria-label={props.chinese ? "助手操作中" : "Assistant controlling"} className="size-2 shrink-0 rounded-full bg-brand" />}
                <IconButton label={`${props.chinese ? "关闭" : "Close"} ${title(tab)}`} className="shrink-0" onClick={() => close(tab.id)}><X className="size-3" /></IconButton>
              </div>)}
            </div>
            <DropdownMenu><DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label={props.chinese ? "添加右侧内容" : "Add workspace content"} />}><Plus /></DropdownMenuTrigger><DropdownMenuContent align="end">
              <DropdownMenuItem disabled={creating} onClick={() => { void createSide(); }}><MessagesSquare />{props.chinese ? "新建旁支" : "New side conversation"}</DropdownMenuItem><DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => { void chooseFile("directory"); }}><FolderOpen />{props.chinese ? "打开文件夹" : "Open folder"}</DropdownMenuItem><DropdownMenuItem onClick={() => { void chooseFile("file"); }}><File />{props.chinese ? "打开文件" : "Open file"}</DropdownMenuItem><DropdownMenuItem onClick={() => { setAddress(""); setAddressOpen(true); }}><Globe />{props.chinese ? "打开网页" : "Open webpage"}</DropdownMenuItem>
            </DropdownMenuContent></DropdownMenu>
            <IconButton label={maximized ? (props.chinese ? "恢复并排显示" : "Restore split view") : (props.chinese ? "展开右侧面板" : "Expand right workspace")} onClick={() => setMaximized(!maximized)}>{maximized ? <Minimize2 /> : <Maximize2 />}</IconButton><IconButton label={props.chinese ? "关闭右侧面板" : "Close right workspace"} onClick={() => setLayout((current) => ({ ...current, open: false }))}><X /></IconButton>
          </div>
          {Object.values(browserActivity).some((activity) => activity.conversationId === sessionId) && <div role="status" className="flex shrink-0 items-center justify-between gap-2 border-b px-3 py-2 text-caption text-muted-foreground"><span>{props.chinese ? "助手操作中 · 使用当前网页登录状态" : "Assistant is using this page and its signed-in session"}</span><Button variant="outline" size="xs" onClick={() => {
            const activity = Object.values(browserActivity).find((activity) => activity.conversationId === sessionId);
            if (activity) void conversationService(transport, props.scope).command(activity.conversationId, { type: "interrupt" }).catch(showError);
          }}>{props.chinese ? "停止" : "Stop"}</Button></div>}
          {openingFiles > 0 && <div role="status" className="flex items-center gap-2 border-b px-3 py-2 text-caption text-muted-foreground"><Loader2 className="size-3.5 animate-spin" />{props.chinese ? "正在定位文件…" : "Locating file…"}</div>}
          {error && <div role="alert" className="flex items-center justify-between border-b px-3 py-2 text-caption text-destructive"><span>{error}</span><IconButton label={props.chinese ? "关闭错误提示" : "Dismiss error"} onClick={() => setError("")}><X /></IconButton></div>}
          {!activeTab && <div className="flex flex-1 flex-col items-center justify-center gap-4 text-body text-muted-foreground"><PanelRightOpen className="size-8" /><span>{props.chinese ? "在这里继续旁支对话，或查看文件和网页" : "Continue a side chat or inspect files and webpages here"}</span><Button variant="outline" disabled={creating} onClick={() => { void createSide(); }}><Plus className="size-4" />{props.chinese ? "新建旁支" : "New side conversation"}</Button></div>}
          {/* Keep Side composers mounted across conversation switches; native previews are released. */}
          {Object.entries(layouts).flatMap(([owner, saved]) => saved.tabs.filter((tab) => owner === key || tab.kind === "side").map((tab) => <section key={`${owner}:${tab.id}`} role="tabpanel" id={`resource-content-${tab.id}`} aria-labelledby={`resource-tab-${tab.id}`} hidden={owner !== key || layout.selected !== tab.id} className="min-h-0 flex-1 overflow-hidden">
            {tab.kind === "side" ? <SideConversation {...props} sessions={sessions} sessionId={tab.sessionId} onSelectSource={(id, turnId, quote) => { setMaximized(false); if (narrow) setLayout((current) => ({ ...current, open: false })); props.onSelectSource(id, turnId, quote); }} onSide={(id) => addTo(owner, { id: `side:${id}`, kind: "side", sessionId: id })} onResource={(id, url) => openResourceFor(owner, id, url)} visible={owner === key && props.active && sideVisible && layout.selected === tab.id} /> : tab.kind === "web" ? <WebResourcePreview id={tab.id} url={tab.url} conversationId={sessions.find((entry) => layoutKey(props.scope, entry.id, device.id) === owner)?.id} scope={props.scope} active={props.active && sideVisible && layout.selected === tab.id} chinese={props.chinese} onChange={(url, title, loading) => setLayout((current) => ({ ...current, tabs: current.tabs.map((item) => item.id === tab.id && item.kind === "web" ? { ...item, url, title, loading } : item) }))} onReference={(url, title) => { props.onReference(`[${title}](${url})`); if (narrow) setLayout((current) => ({ ...current, open: false })); }} /> : <div className="flex h-full min-h-0 min-w-0">
              {tab.location.kind === "directory" && <div className="w-40 shrink-0 overflow-auto border-r p-1 text-label"><PreviewFolder location={tab.location} selected={tab.selected?.path} scope={props.scope} sessionId={tab.sourceId} chinese={props.chinese} onSelect={(selected) => setLayout((current) => ({ ...current, tabs: current.tabs.map((item) => item.id === tab.id && item.kind === "file" ? { ...item, selected, line: undefined } : item) }))} /></div>}
              <div className="min-w-0 flex-1">{tab.selected || tab.location.kind === "file" ? <LocalFilePreview key={(tab.selected ?? tab.location).path} location={tab.selected ?? tab.location} scope={props.scope} sessionId={tab.sourceId} chinese={props.chinese} line={tab.line} /> : <div className="flex h-full items-center justify-center text-body text-muted-foreground">{props.chinese ? "选择文件进行预览" : "Choose a file to preview"}</div>}</div>
            </div>}
          </section>))}
        </div>
      </ResizablePanel>
    </ResizablePanelGroup>
    <Dialog open={addressOpen} onOpenChange={setAddressOpen}><DialogContent><form className="grid gap-4" onSubmit={(event) => { event.preventDefault(); try { const url = new URL(/^https?:\/\//i.test(address) ? address : `https://${address}`); if (!/^https?:$/.test(url.protocol) || url.username || url.password) throw new Error(props.chinese ? "请输入 HTTP 或 HTTPS 地址" : "Enter an HTTP or HTTPS address"); add({ id: `web:${crypto.randomUUID()}`, kind: "web", url: url.href }); setAddressOpen(false); } catch (error) { showError(error); } }}><DialogHeader><DialogTitle>{props.chinese ? "打开网页" : "Open webpage"}</DialogTitle></DialogHeader><label className="grid gap-2 text-label">{props.chinese ? "网页地址" : "Webpage address"}<Input autoFocus value={address} onChange={(event) => setAddress(event.target.value)} placeholder="https://" required /></label><DialogFooter><Button type="button" variant="outline" onClick={() => setAddressOpen(false)}>{props.chinese ? "取消" : "Cancel"}</Button><Button type="submit">{props.chinese ? "打开" : "Open"}</Button></DialogFooter></form></DialogContent></Dialog>
  </div>;
});

function SideConversation(props: Props & { sessionId: string; onSide(id: string): void; onResource(id: string, url: string): Promise<void>; visible: boolean }) {
  const { transport } = useLocalDevice();
  const latest = useRef(props); latest.current = props;
  const session = props.sessions.find((entry) => entry.id === props.sessionId);
  const activity = useSideActivity(props.scope, props.sessionId, props.visible);
  const factory = useCallback((options: ParentServiceOptions) => createLocalConversationParentService({ ...options, locale: props.chinese ? "zh-CN" : "en", storagePrefix: `side:${props.sessionId}:`, getSelectedSessionId: () => props.sessionId, onLocalSelect: (id) => { if (id && id !== props.sessionId) latest.current.onSide(id); }, onOpenSide: (id) => latest.current.onSide(id), onOpenResource: (id, url) => { void latest.current.onResource(id, url).catch((error: unknown) => latest.current.onShowMessage(error instanceof Error ? error.message : String(error), "error")); }, onLocateSource: (id, turnId, quote) => latest.current.onSelectSource(id, turnId, quote) }, transport), [props.sessionId, props.chinese, transport]);
  return <div className="flex h-full min-h-0 flex-col">
    <LocalRuntimeNotice scope={props.scope} chinese={props.chinese} />
    {session?.side && <div className="shrink-0 border-b px-3 py-3 text-caption text-muted-foreground"><div className="flex items-center justify-between gap-2"><span className="min-w-0 truncate">{props.chinese ? "来自主对话" : "From main chat"} · {props.sessions.find((entry) => entry.id === session.side!.sourceId)?.title}</span><Button variant="link" size="xs" onClick={() => props.onSelectSource(session.side!.sourceId, session.side!.turnId, session.side!.quote)}><ArrowLeft className="size-3.5" />{props.chinese ? "定位来源" : "Locate source"}</Button></div>{session.side.quote && <blockquote className="mt-2 max-h-28 overflow-auto border-l-2 pl-3 text-body text-foreground">{session.side.quote}</blockquote>}</div>}
    <SideActivitySummary activity={activity} />
    <div className="relative flex min-h-0 flex-1 flex-col">
      <TuttiAgentGuiPanel serviceFactory={factory} workspaceId={props.scope.workspaceId} workspaceSlug={props.slug} userId={props.scope.userId} activeSessionId={props.sessionId} selectedAgentId={localTargetId(session?.provider ?? props.provider)} active={props.visible} onRestoreDraftApplied={() => {}} onOpenTask={props.onOpenTask} onShowMessage={props.onShowMessage} />
      {session && session.turnCount === 0 && !session.running && <div className="pointer-events-none absolute inset-x-4 top-1/3 flex flex-col items-center gap-3 text-center text-body text-muted-foreground"><MessagesSquare className="size-6" aria-hidden="true" /><p>{props.chinese ? (session.side?.quote ? "围绕选中的内容继续提问" : "在旁支里继续探索") : (session.side?.quote ? "Ask a follow-up about your selection" : "Continue exploring in this side conversation")}</p></div>}
    </div>
  </div>;
}

type WorkspaceTabProps = ComponentProps<typeof Button> & { tab: Tab; chinese: boolean; scope: LocalConversationScope; session?: LocalSessionEntry; active: boolean; title: string };
function WorkspaceTab({ tab, chinese, scope, session, active, title, ...button }: WorkspaceTabProps) {
  if (tab.kind === "side") return <SideConversationTab {...button} chinese={chinese} scope={scope} sessionId={tab.sessionId} session={session} active={active} title={title} />;
  return <Button {...button}>{tab.kind === "web" ? <Globe className="size-4 shrink-0" /> : tab.location.kind === "directory" ? <FolderOpen className="size-4 shrink-0" /> : <File className="size-4 shrink-0" />}<span className="truncate" title={title}>{title}</span></Button>;
}
function SideConversationTab({ chinese: _chinese, scope, sessionId, session, active, title, ...button }: Omit<WorkspaceTabProps, "tab"> & { sessionId: string }) {
  // Running Sides stay live in the tab strip; idle ones follow their summary.
  const activity = useSideActivity(scope, sessionId, active || !!session?.running);
  return <Button {...button} aria-busy={activity.spinning} aria-label={`${title} · ${activity.label}`} title={`${title} · ${activity.label}`}>
    <SideActivityIcon activity={activity} /><span className="truncate">{title}</span>
  </Button>;
}
