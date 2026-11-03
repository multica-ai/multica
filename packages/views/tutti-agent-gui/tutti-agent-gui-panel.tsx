"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { Button } from "@multica/ui/components/ui/button";
import { useAuthStore } from "@multica/core/auth";
import { getCurrentSlug, getCurrentWsId } from "@multica/core/platform";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useWSReconnect } from "@multica/core/realtime";
import type { Agent, ChatSession } from "@multica/core/types";
import type { AgentGuiHostEvent, AgentGuiInitSnapshot, AgentGuiRpcMethod } from "@multica/tutti-agent-gui-host/protocol";
import { agentGuiMessageTargetOrigin, isAgentGuiFrameMessage } from "@multica/tutti-agent-gui-host/frame-messaging";
import { useLocale, useT } from "../i18n";
import { useAppForeground } from "../common/use-app-foreground";
import type { RestoreDraftRequest } from "../chat/components/use-chat-draft-restore";
import { createAgentGuiParentService, type ParentServiceOptions } from "./parent-service";
import { AgentGuiPreviewHost } from "./agent-gui-preview-host";
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

/** The imported UI owns its document, portals, React version and CSS. */
export function TuttiAgentGuiPanel(props: Props) {
  const appOrigin = useAppOrigin();
  const navigation = useOptionalNavigation();
  const resourceNavigation = useRef({ appOrigin, navigation });
  resourceNavigation.current = { appOrigin, navigation };
  const queryClient = useQueryClient();
  const locale = useLocale();
  const { resolvedTheme } = useTheme();
  const { t } = useT("chat");
  const iframe = useRef<HTMLIFrameElement>(null);
  const appForeground = useAppForeground(iframe);
  const active = props.active && appForeground;
  const latest = useRef(props);
  latest.current = { ...props, active };
  const visible = useRef(props.active);
  visible.current = props.active;
  const serviceRef = useRef<ReturnType<typeof createAgentGuiParentService> | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [status, setStatus] = useState<"loading" | "ready" | "error">("loading");
  const [src, setSrc] = useState<string>();
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
    let port: MessagePort | undefined;
    let connecting = false;
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
    const release = service.subscribe((event: AgentGuiHostEvent) => {
      if (current()) port?.postMessage({ type: "event", event });
    });
    const dismissPopovers = (event: PointerEvent) => {
      if (current() && visible.current && event.target instanceof Node && !iframe.current?.contains(event.target)) {
        port?.postMessage({ type: "event", event: { type: "dismissPopovers" } satisfies AgentGuiHostEvent });
      }
    };
    document.addEventListener("pointerdown", dismissPopovers, true);
    let timer: ReturnType<typeof setTimeout>;
    const waitForRender = () => {
      clearTimeout(timer);
      timer = setTimeout(() => { if (alive) setStatus("error"); }, 25_000);
    };
    waitForRender();
    const ready = async (event: MessageEvent) => {
      if (!current() || !isAgentGuiFrameMessage(event, iframe.current?.contentWindow ?? null) || event.data?.version !== 1) return;
      if (event.data.type === "multica:agent-gui:rendered") {
        if (port) { clearTimeout(timer); setStatus("ready"); }
        return;
      }
      if (event.data.type !== "multica:agent-gui:ready" || connecting) return;
      connecting = true;
      setStatus("loading");
      waitForRender();
      port?.close();
      port = undefined;
      try {
        const initial = await snapshot();
        if (!current() || event.source !== iframe.current?.contentWindow) return;
        const channel = new MessageChannel();
        const responsePort = channel.port1;
        const cancelled = new Set<string>();
        port = responsePort;
        responsePort.onmessage = (message: MessageEvent) => {
          const data = message.data;
          if (!current() || !data || typeof data.id !== "string") return;
          if (data.type === "cancel") { cancelled.add(data.id); return; }
          if (data.type !== "request" || typeof data.method !== "string") return;
          const backgroundAllowed = data.method === "workspace.load" || data.method === "session.load"
            || data.method === "session.watch" || data.method === "task.messages"
            // Local history and its event stream must survive startup and focus changes.
            || data.method === "local.snapshot" || data.method === "local.storedSnapshot" || data.method === "local.subscribe" || data.method === "local.unsubscribe"
            // A transfer outlives the focus that started it.
            || data.method === "attachment.cancel" || data.method === "attachment.retry" || data.method === "attachment.forget" || data.method === "attachment.url" || data.method === "image.url"
            || data.method === "attachment.read" || data.method === "image.read" || data.method === "context.tasks" || data.method === "host.promptApplied"
            || data.method === "host.showMessage" || data.method.startsWith("storage.")
            // A prompt admitted to the queue must survive focus and tab changes.
            || (["session.send", "queue.enqueue", "host.openSide", "host.startSide"].includes(data.method) && data.params?.queued === true);
          const interactive = visible.current && document.visibilityState === "visible" && document.hasFocus();
          if (!interactive && !backgroundAllowed) {
            responsePort.postMessage({ type: "response", id: data.id, error: { message: "This Agent GUI tab is inactive", code: "inactive_host", status: 409 } });
            return;
          }
          void service.request(data.method as AgentGuiRpcMethod, data.params).then(
            (result) => { if (current() && port === responsePort && !cancelled.delete(data.id)) responsePort.postMessage({ type: "response", id: data.id, result }); },
            (error: unknown) => {
              if (!current() || port !== responsePort || cancelled.delete(data.id)) return;
              const detail = error as { message?: string; code?: string; status?: number };
              responsePort.postMessage({ type: "response", id: data.id, error: {
                message: detail?.message || "GUI request failed", code: detail?.code, status: detail?.status,
              } });
            },
          );
        };
        port.start();
        iframe.current!.contentWindow!.postMessage({ type: "multica:agent-gui:connect", snapshot: initial },
          agentGuiMessageTargetOrigin(), [channel.port2]);
      } catch { if (alive) setStatus("error"); }
      finally { connecting = false; }
    };
    window.addEventListener("message", ready);
    // File builds resolve beside renderer/index.html; routed HTTP builds use the public root.
    const base = window.location.protocol === "file:"
      ? new URL("./tutti-agent-gui/index.html", window.location.href).href
      : new URL("/tutti-agent-gui/index.html", window.location.origin).href;
    // A new host lifetime needs a new child handshake; retained tabs keep this document.
    setSrc(`${base}?connection=${crypto.randomUUID()}`);
    return () => {
      alive = false;
      clearTimeout(timer);
      window.removeEventListener("message", ready);
      document.removeEventListener("pointerdown", dismissPopovers, true);
      release(); port?.close(); service.dispose();
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

  return <div className="relative flex min-h-0 flex-1 flex-col" data-testid="tutti-agent-gui-host">
    <AgentGuiPreviewHost iframe={iframe} enabled={props.active} isCurrent={() => getCurrentSlug() === workspaceSlug && getCurrentWsId() === workspaceId && useAuthStore.getState().user?.id === userId}>
    {src && <iframe key={attempt} ref={iframe} src={src} title={t(($) => $.workbench.title)} className="h-full min-h-0 w-full flex-1 border-0" />}
    {status !== "ready" && <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-background" role="status">
      <span>{t(($) => status === "error" ? $.workbench.load_error : $.workbench.loading)}</span>
      {status === "error" && <Button variant="outline" onClick={() => setAttempt((value) => value + 1)}>{t(($) => $.workbench.retry)}</Button>}
    </div>}
    </AgentGuiPreviewHost>
  </div>;
}
