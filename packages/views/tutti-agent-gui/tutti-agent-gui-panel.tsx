"use client";

import { Component, Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { Button } from "@multica/ui/components/ui/button";
import { useAuthStore } from "@multica/core/auth";
import { getCurrentSlug, getCurrentWsId } from "@multica/core/platform";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useWSReconnect } from "@multica/core/realtime";
import type { Agent, ChatSession } from "@multica/core/types";
import type { AgentGuiHostEvent, AgentGuiInitSnapshot, AgentGuiRpcClient } from "@multica/tutti-agent-gui-host/protocol";
import { createInProcessRpcClient } from "@multica/tutti-agent-gui-host/in-process-client";
import { useLocale, useT } from "../i18n";
import { useAppForeground } from "../common/use-app-foreground";
import type { RestoreDraftRequest } from "../chat/components/use-chat-draft-restore";
import { createAgentGuiParentService, type ParentServiceOptions } from "./parent-service";
import { useAppOrigin, useOptionalNavigation } from "../navigation";

interface Props {
  locateRequest?: { sessionId: string; turnId?: string; quote?: string; sequence: number };
  serviceFactory?: (options: ParentServiceOptions) => ReturnType<typeof createAgentGuiParentService>;
  workspaceId: string;
  workspaceSlug: string;
  userId: string;
  activeSessionId: string | null;
  selectedAgentId?: string | null;
  active: boolean;
  composerFocusSequence?: number;
  newConversationRequest?: AgentGuiInitSnapshot["newConversationRequest"];
  restoreDraftRequest?: RestoreDraftRequest | null;
  onRestoreDraftApplied: (id: string) => void;
  onSelectSession: (session: ChatSession | null, agent?: Agent) => void;
  onOpenTask: (issueId: string) => void;
  onShowMessage: (message: string, tone?: "success" | "info" | "warning" | "error") => void;
}

class LoadBoundary extends Component<{ onError(): void; children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch() { this.props.onError(); }
  render() { return this.state.failed ? null : this.props.children; }
}

/** The imported GUI is loaded on demand: it is large, and it only runs in the browser. */
export function TuttiAgentGuiPanel(props: Props) {
  const appOrigin = useAppOrigin();
  const navigation = useOptionalNavigation();
  const resourceNavigation = useRef({ appOrigin, navigation });
  resourceNavigation.current = { appOrigin, navigation };
  const queryClient = useQueryClient();
  const locale = useLocale();
  const { resolvedTheme } = useTheme();
  const { t } = useT("chat");
  const container = useRef<HTMLDivElement>(null);
  const appForeground = useAppForeground();
  const active = props.active && appForeground;
  const latest = useRef(props);
  latest.current = { ...props, active };
  const visible = useRef(props.active);
  visible.current = props.active;
  const serviceRef = useRef<ReturnType<typeof createAgentGuiParentService> | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [connection, setConnection] = useState<{ client: AgentGuiRpcClient; initial: AgentGuiInitSnapshot } | null>(null);
  // A failed chunk load is cached by its lazy component, so a retry needs a new one.
  const AgentGuiMount = useMemo(() => { void attempt; return lazy(() => import("@multica/tutti-agent-gui-host/mount")); }, [attempt]);
  const renderTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const sequence = useRef(Date.now());
  const sentRestore = useRef<string | null>(null);
  const initRef = useRef({ locale, theme: resolvedTheme, activeSessionId: props.activeSessionId, selectedAgentId: props.selectedAgentId, composerFocusSequence: props.composerFocusSequence, newConversationRequest: props.newConversationRequest });
  initRef.current = { locale, theme: resolvedTheme, activeSessionId: props.activeSessionId, selectedAgentId: props.selectedAgentId, composerFocusSequence: props.composerFocusSequence, newConversationRequest: props.newConversationRequest };
  const { workspaceId, workspaceSlug, userId } = props;

  const snapshot = useCallback(async (): Promise<AgentGuiInitSnapshot> => {
    const service = serviceRef.current;
    if (!service) throw new Error("GUI host disconnected");
    const workspace = await service.load();
    const state = initRef.current;
    return { ...workspace, workspaceId, workspaceSlug, userId, appOrigin: resourceNavigation.current.appOrigin,
      activeSessionId: state.activeSessionId, selectedAgentId: state.selectedAgentId,
      composerFocusSequence: state.composerFocusSequence,
      newConversationRequest: state.newConversationRequest,
      hostActive: latest.current.active,
      hostVisible: visible.current,
      locale: state.locale.startsWith("zh") ? "zh-CN" : "en",
      theme: state.theme === "dark" ? "dark" : "light" };
  }, [workspaceId, workspaceSlug, userId]);

  useEffect(() => {
    let alive = true;
    const current = () => alive
      && getCurrentSlug() === workspaceSlug && getCurrentWsId() === workspaceId
      && useAuthStore.getState().user?.id === userId;
    const service = (props.serviceFactory ?? createAgentGuiParentService)({
      queryClient, workspaceId, workspaceSlug, userId, isCurrent: current,
      resolveAvatar: (url) => resolvePublicFileUrl(url) ?? url,
      onSelectSession: (session, agent) => latest.current.onSelectSession(session, agent),
      onOpenTask: (id) => latest.current.onOpenTask(id),
      getAppOrigin: () => resourceNavigation.current.appOrigin,
      onOpenInternalResource: (path, background) => {
        const adapter = resourceNavigation.current.navigation;
        if (!adapter) throw new Error("Internal navigation is unavailable");
        if (adapter.openInNewTab) adapter.openInNewTab(path, undefined, { activate: !background });
        else adapter.push(path);
      },
      onShowMessage: (message, tone) => latest.current.onShowMessage(message, tone),
      onPromptApplied: (_sequence, restoreId) => {
        if (restoreId && latest.current.restoreDraftRequest?.id === restoreId) latest.current.onRestoreDraftApplied(restoreId);
      },
    });
    serviceRef.current = service;
    sentRestore.current = null;
    setStatus("loading");
    setConnection(null);
    const inProcess = createInProcessRpcClient(service, {
      current,
      // Only the visible, focused instance acts for the user.
      interactive: () => visible.current && document.visibilityState === "visible" && document.hasFocus(),
    });
    const dismissPopovers = (event: PointerEvent) => {
      if (current() && visible.current && event.target instanceof Node && !container.current?.contains(event.target)) {
        service.publish({ type: "dismissPopovers" } satisfies AgentGuiHostEvent);
      }
    };
    document.addEventListener("pointerdown", dismissPopovers, true);
    let initializing = false;
    let initialized = false;
    const initialize = () => {
      if (!current() || initializing || initialized) return;
      initializing = true;
      setStatus("loading");
      clearTimeout(renderTimer.current);
      renderTimer.current = setTimeout(() => { if (alive) setStatus("error"); }, 25_000);
      void snapshot().then(
        (initial) => {
          if (!current()) return;
          initialized = true;
          setConnection({ client: inProcess.client, initial });
        },
        () => { if (current()) setStatus("error"); },
      ).finally(() => { initializing = false; });
    };
    // Before the first snapshot there is no mounted GUI to consume recovery events.
    const stopInitializationRecovery = service.subscribe((event) => {
      if ((event.type === "invalidate" && event.scope === "workspace") || (event.type === "connection" && event.connected)) initialize();
    });
    initialize();
    return () => {
      alive = false;
      clearTimeout(renderTimer.current);
      stopInitializationRecovery();
      document.removeEventListener("pointerdown", dismissPopovers, true);
      inProcess.dispose();
      service.dispose();
      if (serviceRef.current === service) serviceRef.current = null;
    };
  }, [workspaceId, workspaceSlug, userId, attempt, queryClient, snapshot, props.serviceFactory]);

  useEffect(() => {
    if (status !== "ready") return;
    let alive = true;
    void snapshot().then((value) => { if (alive) serviceRef.current?.publish({ type: "init", snapshot: value }); }).catch(() => undefined);
    return () => { alive = false; };
  }, [snapshot, locale, resolvedTheme, props.activeSessionId, props.selectedAgentId, props.composerFocusSequence, props.newConversationRequest, props.active, active, status, appOrigin]);

  useEffect(() => { if (active) serviceRef.current?.reconnect(); }, [active]);
  useEffect(() => {
    if (status === "ready" && props.locateRequest?.sessionId === props.activeSessionId) serviceRef.current?.publish({ type: "locateSource", ...props.locateRequest });
  }, [status, props.locateRequest, props.activeSessionId]);

  useEffect(() => {
    const restore = props.restoreDraftRequest;
    if (!active || status !== "ready" || !restore || sentRestore.current === restore.id) return;
    if (restore.sessionId && restore.sessionId !== props.activeSessionId) return;
    sentRestore.current = restore.id;
    serviceRef.current?.publish({ type: "appendPrompt", sequence: ++sequence.current,
      prompt: restore.content, attachments: restore.attachments, sessionId: restore.sessionId,
      restoreRequestId: restore.id });
  }, [props.restoreDraftRequest, props.activeSessionId, active, status]);

  useWSReconnect(useCallback(() => serviceRef.current?.reconnect(), []));

  const rendered = useCallback(() => { clearTimeout(renderTimer.current); setStatus("ready"); }, []);
  return <div ref={container} className="relative flex min-h-0 flex-1 flex-col" data-testid="tutti-agent-gui-host">
    {connection && <LoadBoundary key={attempt} onError={() => setStatus("error")}><Suspense fallback={null}><AgentGuiMount client={connection.client} initial={connection.initial} localConversations={serviceRef.current?.localConversations} onRendered={rendered} /></Suspense></LoadBoundary>}
    {status !== "ready" && <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-background" role="status">
      <span>{t(($) => status === "error" ? $.workbench.load_error : $.workbench.loading)}</span>
      {status === "error" && <Button variant="outline" onClick={() => setAttempt((value) => value + 1)}>{t(($) => $.workbench.retry)}</Button>}
    </div>}
  </div>;
}
