import { useEffect, useId, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { AgentGUI, type AgentGUIProps } from "@tutti-os/agent-gui/agent-gui";
import { resolveAgentGUIProviderIdentity } from "@tutti-os/agent-gui/provider-identity";
import { resolveProviderIconAsset } from "@tutti-os/agent-gui/provider-icons";
import { MAX_FILE_SIZE } from "@multica/core/constants/upload";
import genericAgentIcon from "../../../vendor/tutti/packages/agent/gui/app/renderer/assets/icons/agents/agent-colorful.png";
import { executionTargets } from "./protocol";
import type { AgentGuiInitSnapshot, AgentGuiRpcClient } from "./protocol";
import type { createMulticaAgentGuiRuntime } from "./runtime";
import { createAgentGuiHostApi } from "./host-api";
import { ComposerFooterControls, type ProviderDraft } from "./composer-footer-controls";
import { createMulticaContextMentions } from "./context-mentions";
import { SideEntryProvider } from "./side-entry";
import { ResourceLinkProvider } from "./resource-link";
import { ComposerDraftProvider } from "./composer-drafts";
import { InvokedAgentsProvider } from "./invoked-agent-reply";
import { applyAgentGuiAppearance, syncAgentGuiDocument } from "./agent-gui-appearance";
import { dispatchAgentGuiWorkbenchCommand } from "../../../vendor/tutti/packages/agent/gui/workbench/commands";

type RuntimeHandle = ReturnType<typeof createMulticaAgentGuiRuntime>;
type NodeState = AgentGUIProps["state"];
const HIDDEN_MENTION_FILTER_IDS = ["file", "app"] as const;

function initialNodeState(snapshot: AgentGuiInitSnapshot, handle: RuntimeHandle): NodeState {
  const selectedId = snapshot.selectedAgentId
    ?? snapshot.sessions.find((session) => session.id === snapshot.activeSessionId)?.agent_id;
  const descriptor = executionTargets(snapshot).find((entry) => entry.id === selectedId)
    ?? (!selectedId ? executionTargets(snapshot).find((entry) => entry.canInvoke && entry.runtimeBound)
      ?? executionTargets(snapshot)[0] : undefined);
  return {
    provider: descriptor?.provider ?? "",
    agentTargetId: descriptor?.id ?? null,
    lastActiveAgentSessionId: snapshot.activeSessionId
      ? handle.toGuiSessionId(snapshot.activeSessionId)
      : null,
    conversationRailCollapsed: true,
  };
}

export function AgentGuiApplication({
  client,
  initial,
  handle,
  root,
}: {
  client: AgentGuiRpcClient;
  initial: AgentGuiInitSnapshot;
  handle: RuntimeHandle;
  root: HTMLElement;
}) {
  const [snapshot, setSnapshot] = useState(initial);
  const [composerFocusSequence, setComposerFocusSequence] = useState<number | null>(null);
  const workspace = useSyncExternalStore(handle.subscribeWorkspace, handle.getWorkspaceState);
  const [node, setNode] = useState<NodeState>(() => initialNodeState(initial, handle));
  // The composer remounts between an existing conversation and its home.
  // Keep the requested target outside that composer until the old session exits.
  const [pendingTarget, setPendingTarget] = useState<{ target: ReturnType<typeof executionTargets>[number]; fromSession: string; draft?: ProviderDraft; projectPath?: string | null } | null>(null);
  const [pendingNewConversation, setPendingNewConversation] = useState(() => initial.local && !initial.activeSessionId ? initial.newConversationRequest : undefined);
  const lastNewConversation = useRef(initial.activeSessionId ? undefined : initial.newConversationRequest?.sequence);
  const nodeRef = useRef(node);
  nodeRef.current = node;
  const lastSelection = useRef({ sessionId: initial.activeSessionId, agentId: node.agentTargetId });
  const [appends, setAppends] = useState<NonNullable<AgentGUIProps["runtimeRequests"]["composerAppend"]>[]>([]);
  const appendRestoreIds = useRef(new Map<number, string | undefined>());
  const appliedAppends = useRef(new Set<number>());
  const previousInit = useRef(initial);
  const [size, setSize] = useState(() => ({ width: root.clientWidth, height: root.clientHeight }));
  // Several instances share the document, so one's own identity keeps its workbench commands apart.
  const instanceId = `multica-agent-gui:${initial.workspaceId}:${useId()}`;

  useEffect(() => {
    applyAgentGuiAppearance(root, snapshot);
    syncAgentGuiDocument(snapshot);
  }, [root, snapshot.locale, snapshot.theme, snapshot.sidePane]);

  useEffect(() => {
    // A hidden instance measures zero; it keeps the size it last showed.
    const observer = new ResizeObserver(() => { if (root.clientWidth && root.clientHeight) setSize({ width: root.clientWidth, height: root.clientHeight }); });
    observer.observe(root);
    return () => observer.disconnect();
  }, [root]);

  useEffect(() => client.subscribe((event) => {
    if (event.type === "init") {
      if (event.snapshot.workspaceId !== initial.workspaceId || event.snapshot.userId !== initial.userId) return;
      setSnapshot(event.snapshot);
      const currentServerId = nodeRef.current.lastActiveAgentSessionId
        ? handle.toServerSessionId(nodeRef.current.lastActiveAgentSessionId)
        : null;
      const agentIntentChanged = event.snapshot.selectedAgentId !== previousInit.current.selectedAgentId;
      const newConversationChanged = event.snapshot.local && !event.snapshot.activeSessionId
        && event.snapshot.newConversationRequest?.sequence !== lastNewConversation.current;
      if (event.snapshot.activeSessionId) setPendingNewConversation(undefined);
      else if (newConversationChanged) {
        lastNewConversation.current = event.snapshot.newConversationRequest?.sequence;
        setPendingNewConversation(event.snapshot.newConversationRequest);
        setPendingTarget(null);
      }
      previousInit.current = event.snapshot;
      if (event.snapshot.activeSessionId !== currentServerId || agentIntentChanged || newConversationChanged) {
        if (event.snapshot.activeSessionId) setPendingTarget(null);
        const external = initialNodeState(event.snapshot, handle);
        lastSelection.current = { sessionId: event.snapshot.activeSessionId, agentId: external.agentTargetId };
        setNode((current) => ({ ...current, ...external }));
      }
    } else if (event.type === "appendPrompt") {
      if (appliedAppends.current.has(event.sequence)) return;
      handle.registerAttachments?.(event.attachments ?? []);
      appendRestoreIds.current.set(event.sequence, event.restoreRequestId);
      const next: NonNullable<AgentGUIProps["runtimeRequests"]["composerAppend"]> = {
        sequence: event.sequence,
        ...(event.sessionId ? { agentSessionId: handle.toGuiSessionId(event.sessionId) } : {}),
        prompt: event.prompt,
        ...(event.attachments?.length ? {
          files: event.attachments.map((attachment) => ({
            id: attachment.id,
            name: attachment.filename,
            mimeType: attachment.content_type,
            url: attachment.download_url || attachment.markdown_url || attachment.url,
            assetId: attachment.id,
            sizeBytes: attachment.size_bytes,
            uploadStatus: "ready",
          })),
        } : {}),
      };
      setAppends((current) => current.some((request) => request.sequence === next.sequence) ? current : [...current, next]);
    }
  }), [client, handle, initial.userId, initial.workspaceId]);

  useEffect(() => {
    const sessionId = node.lastActiveAgentSessionId
      ? handle.toServerSessionId(node.lastActiveAgentSessionId)
      : null;
    if (node.lastActiveAgentSessionId && !sessionId) return;
    const agentId = node.agentTargetId ?? undefined;
    if (sessionId === lastSelection.current.sessionId && agentId === lastSelection.current.agentId) return;
    lastSelection.current = { sessionId, agentId };
    void client.request("host.selectSession", { sessionId, agentId }).catch(() => undefined);
  }, [client, handle, node.agentTargetId, node.lastActiveAgentSessionId, workspace.sessions]);

  const agentDirectory = useMemo<AgentGUIProps["agentDirectory"]>(() => ({
    status: "ready",
    error: null,
    capturedAtUnixMs: Date.now(),
    agents: executionTargets(workspace).map((descriptor) => {
      const identity = resolveAgentGUIProviderIdentity(descriptor.provider);
      const iconUrl = descriptor.iconUrl
        || resolveProviderIconAsset(identity?.iconKey, "providerRail")
        || genericAgentIcon;
      return {
        agentTargetId: descriptor.id,
        provider: descriptor.provider,
        name: descriptor.name,
        description: descriptor.description,
        iconUrl,
        maskIconUrl: !descriptor.iconUrl
          ? resolveProviderIconAsset(identity?.iconKey, "sessionFlat")
          : null,
        providerAccountUsageApplicable: false,
        availability: {
          status: descriptor.canInvoke && descriptor.runtimeBound ? "ready" : "unavailable",
          reason: descriptor.unavailableReason,
        },
      };
    }),
  }), [workspace]);
  const mentionAgentDirectory = useMemo<NonNullable<AgentGUIProps["mentionAgentDirectory"]>>(() => {
    const agents = new Map(agentDirectory.agents.map((entry) => [entry.agentTargetId, entry]));
    for (const { agent, provider, iconUrl, canInvoke, runtimeBound, unavailableReason } of workspace.agents) {
      agents.set(agent.id, {
        agentTargetId: agent.id,
        provider,
        name: agent.name,
        description: agent.description,
        iconUrl: iconUrl || genericAgentIcon,
        providerAccountUsageApplicable: false,
        availability: {
          status: canInvoke && runtimeBound && !agent.archived_at ? "ready" : "unavailable",
          reason: unavailableReason,
        },
      });
    }
    return { ...agentDirectory, agents: [...agents.values()] };
  }, [agentDirectory, workspace.agents]);
  const activeSessionId = node.lastActiveAgentSessionId;
  // The empty composer's send button continues a stopped or interrupted local turn.
  const continuable = useSyncExternalStore(handle.subscribeTurns,
    () => snapshot.local && activeSessionId ? handle.continuable(activeSessionId) : false);
  const composerEmptySubmit = useMemo(() => continuable && activeSessionId
    ? { label: snapshot.locale === "zh-CN" ? "继续" : "Continue", onSubmit: () => handle.continueSession(activeSessionId) }
    : null, [continuable, activeSessionId, handle, snapshot.locale]);
  const imageSessionId = node.lastActiveAgentSessionId ? handle.toServerSessionId(node.lastActiveAgentSessionId) : null;
  const hostApi = useMemo(() => createAgentGuiHostApi(client, { workspaceId: snapshot.workspaceId, locale: snapshot.locale, local: snapshot.local }, handle, imageSessionId), [client, handle, snapshot.locale, snapshot.workspaceId, snapshot.local, imageSessionId]);
  const mentionService = useMemo(() => createMulticaContextMentions(client, { workspaceId: snapshot.workspaceId, locale: snapshot.locale, local: snapshot.local }, handle), [client, handle, snapshot.workspaceId, snapshot.locale, snapshot.local]);
  useEffect(() => () => mentionService.dispose(), [mentionService]);
  const prepareFiles = useMemo<NonNullable<AgentGUIProps["workspace"]["prepareExternalPromptFiles"]>>(() => async (files) => Promise.all(files.map(async (file, sourceIndex) => {
    if (file.size > MAX_FILE_SIZE) return { sourceIndex, status: "error" as const, errorCode: "file_too_large" as const, retryable: false };
    try {
      const attachment = await handle.uploadAttachment(file);
      return { sourceIndex, status: "prepared" as const, file: {
        name: attachment.filename,
        url: attachment.markdown_url || attachment.url || attachment.download_url,
        assetId: attachment.id,
        mimeType: attachment.content_type,
        sizeBytes: attachment.size_bytes,
        uploadStatus: "uploaded",
      } };
    } catch {
      return { sourceIndex, status: "error" as const, errorCode: "preparation_failed" as const, retryable: true };
    }
  })), [handle]);

  return <ComposerDraftProvider initial={initial}><InvokedAgentsProvider value={workspace.agents}><ResourceLinkProvider client={client} scope={{ workspaceId: initial.workspaceId, workspaceSlug: initial.workspaceSlug, appOrigin: snapshot.appOrigin }} agents={workspace.agents}><SideEntryProvider root={root} enabled={snapshot.sideWorkspace === true} sidePane={snapshot.sidePane} client={client} handle={handle} sessionId={node.lastActiveAgentSessionId ? handle.toServerSessionId(node.lastActiveAgentSessionId) : null} chinese={snapshot.locale === "zh-CN"}><AgentGUI
    identity={{ nodeId: instanceId, workspaceId: snapshot.workspaceId, currentUserId: snapshot.userId, title: "Agent GUI" }}
    workspace={{ path: "", agentSettings: { avoidGroupingEdits: false }, prepareExternalPromptFiles: prepareFiles }}
    frame={{ position: { x: 0, y: 0 }, ...size, desktopSize: size, isActive: snapshot.hostActive ?? true, isVisible: snapshot.hostVisible ?? true, embedded: true }}
    state={node}
    runtimeRequests={{ composerAppend: appends[0] ?? null, composerFocusSequence, workbench: { instanceId } }}
    agentDirectory={agentDirectory}
    mentionAgentDirectory={mentionAgentDirectory}
    agentActivityRuntime={handle.runtime}
    agentHostApi={hostApi}
    locale={snapshot.locale}
    disabled={["meet-tutti", "import-session"]}
    hostCapabilities={{
      hiddenMentionFilterIds: HIDDEN_MENTION_FILTER_IDS,
      mentionService,
      sessionWorktreeEnabled: false,
      sideConversationEnabled: false,
      capabilityControlsReadOnly: true,
      capabilityMenuState: { tuttiMode: { enabled: false }, connectors: { enabled: false }, computerUse: { presentationSupported: false } },
    }}
    hostActions={{
      onClose: () => undefined,
      onResize: () => undefined,
      onUpdateNode: setNode,
      onShowMessage: (message, tone) => { void client.request("host.showMessage", { message, tone }); },
      onComposerAppendHandled: (sequence) => {
        const restoreRequestId = appendRestoreIds.current.get(sequence);
        appendRestoreIds.current.delete(sequence);
        appliedAppends.current.add(sequence);
        setAppends((current) => current.filter((request) => request.sequence !== sequence));
        void client.request("host.promptApplied", { sequence, restoreRequestId });
      },
    }}
    renderSlots={{ composerEmptySubmit, composerFooterAccessory: (context) => <ComposerFooterControls context={context} client={client} handle={handle} targets={executionTargets(workspace)} snapshot={snapshot} onReadyForFocus={setComposerFocusSequence}
      newConversationRequest={pendingNewConversation} onNewConversationApplied={() => setPendingNewConversation(undefined)}
      onOpenNewConversation={() => dispatchAgentGuiWorkbenchCommand({ type: "new-conversation", instanceId })}
      pendingTarget={pendingTarget} onCancelTargetSwitch={() => setPendingTarget(null)}
      onLeaveSession={(target, fromSession, draft, projectPath) => { setPendingTarget({ target, fromSession, draft, projectPath }); setNode((current) => ({ ...current, lastActiveAgentSessionId: null })); }}
      onSelectAgent={(target) => { setPendingTarget(null); setNode((current) => current.agentTargetId === target.id ? current : { ...current, provider: target.provider, agentTargetId: target.id, lastActiveAgentSessionId: null }); }} /> }}
  /></SideEntryProvider></ResourceLinkProvider></InvokedAgentsProvider></ComposerDraftProvider>;
}
