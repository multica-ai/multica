import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { MAX_FILE_SIZE } from "@multica/core/constants/upload";
import { AgentGUI, type AgentGUIProps } from "@tutti-os/agent-gui/agent-gui";
import { resolveProviderIconAsset } from "@tutti-os/agent-gui/provider-icons";
import { resolveAgentGUIProviderIdentity } from "@tutti-os/agent-gui/provider-identity";
import {
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore
} from "react";
import genericAgentIcon from "../../../vendor/tutti/packages/agent/gui/app/renderer/assets/icons/agents/agent-colorful.png";
import { dispatchAgentGuiWorkbenchCommand } from "../../../vendor/tutti/packages/agent/gui/workbench/commands";
import {
  applyAgentGuiAppearance,
  syncAgentGuiDocument
} from "./agent-gui-appearance";
import { ComposerDraftProvider } from "./composer-drafts";
import { composerCommands, type ComposerCommandRequest } from "./composer-commands";
import {
  ComposerFooterControls,
  type ProviderDraft
} from "./composer-footer-controls";
import { createMulticaContextMentions } from "./context-mentions";
import { createAgentGuiHostApi } from "./host-api";
import { InvokedAgentsProvider } from "./invoked-agent-reply";
import type { AgentGuiHostServices, AgentGuiInitSnapshot } from "./protocol";
import { executionTargets } from "./protocol";
import { ResourceLinkProvider } from "./resource-link";
import type { createMulticaAgentGuiRuntime } from "./runtime";
import { SideEntryProvider, type SelectedQuote } from "./side-entry";
import { observer } from "mobx-react-lite";
export const AgentGuiApplication = observer(AgentGuiApplicationRender);

type RuntimeHandle = ReturnType<typeof createMulticaAgentGuiRuntime>;
type NodeState = AgentGUIProps["state"];
const HIDDEN_MENTION_FILTER_IDS = ["file", "app"] as const;

function initialNodeState(
  snapshot: AgentGuiInitSnapshot,
  handle: RuntimeHandle
): NodeState {
  const selectedId =
    snapshot.selectedAgentId ??
    snapshot.sessions.find((session) => session.id === snapshot.activeSessionId)
      ?.agent_id;
  const descriptor =
    executionTargets(snapshot).find((entry) => entry.id === selectedId) ??
    (!selectedId
      ? (executionTargets(snapshot).find(
          (entry) => entry.canInvoke && entry.runtimeBound
        ) ?? executionTargets(snapshot)[0])
      : undefined);
  return {
    provider: descriptor?.provider ?? "",
    agentTargetId: descriptor?.id ?? null,
    lastActiveAgentSessionId: snapshot.activeSessionId
      ? handle.toGuiSessionId(snapshot.activeSessionId)
      : null,
    conversationRailCollapsed: true
  };
}

function AgentGuiApplicationRender({
  client,
  initial,
  handle,
  root
}: {
  client: AgentGuiHostServices;
  initial: AgentGuiInitSnapshot;
  handle: RuntimeHandle;
  root: HTMLElement;
}) {
  const [snapshot, setSnapshot] = useState(initial);
  const [composerCommand, setComposerCommand] = useState<ComposerCommandRequest | null>(null);
  const commandSequence = useRef(0);
  const [composerFocusSequence, setComposerFocusSequence] = useState<
    number | null
  >(null);
  const workspace = useSyncExternalStore(
    handle.subscribeWorkspace,
    handle.getWorkspaceState
  );
  const [node, setNode] = useState<NodeState>(() =>
    initialNodeState(initial, handle)
  );
  // Keep the requested target until the composer has left the old session scope.
  const [pendingTarget, setPendingTarget] = useState<{
    target: ReturnType<typeof executionTargets>[number];
    fromSession: string;
    draft?: ProviderDraft;
    projectPath?: string | null;
  } | null>(null);
  const [pendingNewConversation, setPendingNewConversation] = useState(() =>
    initial.local && !initial.activeSessionId
      ? initial.newConversationRequest
      : undefined
  );
  const lastNewConversation = useRef(
    initial.activeSessionId
      ? undefined
      : initial.newConversationRequest?.sequence
  );
  const nodeRef = useRef(node);
  nodeRef.current = node;
  const lastSelection = useRef({
    sessionId: initial.activeSessionId,
    agentId: node.agentTargetId
  });
  const [appends, setAppends] = useState<
    NonNullable<AgentGUIProps["runtimeRequests"]["composerAppend"]>[]
  >([]);
  const appendRestoreIds = useRef(new Map<number, string | undefined>());
  // Host append sequences are positive; local selection requests stay in this instance.
  const localAppendSequence = useRef(0);
  const appliedAppends = useRef(new Set<number>());
  const previousInit = useRef(initial);
  const [size, setSize] = useState(() => ({
    width: root.clientWidth,
    height: root.clientHeight
  }));
  // Several instances share the document, so one's own identity keeps its workbench commands apart.
  const instanceId = `multica-agent-gui:${initial.workspaceId}:${useId()}`;

  useEffect(() => {
    applyAgentGuiAppearance(root, snapshot);
    syncAgentGuiDocument(snapshot);
  }, [root, snapshot.locale, snapshot.theme, snapshot.sidePane]);

  useEffect(() => {
    // A hidden instance measures zero; it keeps the size it last showed.
    const observer = new ResizeObserver(() => {
      if (root.clientWidth && root.clientHeight)
        setSize({ width: root.clientWidth, height: root.clientHeight });
    });
    observer.observe(root);
    return () => observer.disconnect();
  }, [root]);

  useEffect(
    () =>
      client.subscribe((event) => {
        if (event.type === "init") {
          if (
            event.snapshot.workspaceId !== initial.workspaceId ||
            event.snapshot.userId !== initial.userId
          )
            return;
          setSnapshot(event.snapshot);
          const currentServerId = nodeRef.current.lastActiveAgentSessionId
            ? handle.toServerSessionId(nodeRef.current.lastActiveAgentSessionId)
            : null;
          const agentIntentChanged =
            event.snapshot.selectedAgentId !==
            previousInit.current.selectedAgentId;
          const newConversationChanged =
            event.snapshot.local &&
            !event.snapshot.activeSessionId &&
            event.snapshot.newConversationRequest?.sequence !==
              lastNewConversation.current;
          if (event.snapshot.activeSessionId)
            setPendingNewConversation(undefined);
          else if (newConversationChanged) {
            lastNewConversation.current =
              event.snapshot.newConversationRequest?.sequence;
            setPendingNewConversation(event.snapshot.newConversationRequest);
            setPendingTarget(null);
          }
          previousInit.current = event.snapshot;
          if (
            event.snapshot.activeSessionId !== currentServerId ||
            agentIntentChanged ||
            newConversationChanged
          ) {
            if (event.snapshot.activeSessionId) setPendingTarget(null);
            const external = initialNodeState(event.snapshot, handle);
            lastSelection.current = {
              sessionId: event.snapshot.activeSessionId,
              agentId: external.agentTargetId
            };
            setNode((current) => ({ ...current, ...external }));
          }
        } else if (event.type === "appendPrompt") {
          if (appliedAppends.current.has(event.sequence)) return;
          handle.registerAttachments?.(event.attachments ?? []);
          appendRestoreIds.current.set(event.sequence, event.restoreRequestId);
          const next: NonNullable<
            AgentGUIProps["runtimeRequests"]["composerAppend"]
          > = {
            sequence: event.sequence,
            ...(event.sessionId
              ? { agentSessionId: handle.toGuiSessionId(event.sessionId) }
              : {}),
            prompt: event.prompt,
            ...(event.attachments?.length
              ? {
                  files: event.attachments.map((attachment) => ({
                    id: attachment.id,
                    name: attachment.filename,
                    mimeType: attachment.content_type,
                    url:
                      attachment.download_url ||
                      attachment.markdown_url ||
                      attachment.url,
                    assetId: attachment.id,
                    sizeBytes: attachment.size_bytes,
                    uploadStatus: "ready"
                  }))
                }
              : {})
          };
          setAppends((current) =>
            current.some((request) => request.sequence === next.sequence)
              ? current
              : [...current, next]
          );
        }
      }),
    [client, handle, initial.userId, initial.workspaceId]
  );

  useEffect(() => {
    const sessionId = node.lastActiveAgentSessionId
      ? handle.toServerSessionId(node.lastActiveAgentSessionId)
      : null;
    if (node.lastActiveAgentSessionId && !sessionId) return;
    const agentId = node.agentTargetId ?? undefined;
    if (
      sessionId === lastSelection.current.sessionId &&
      agentId === lastSelection.current.agentId
    )
      return;
    lastSelection.current = { sessionId, agentId };
    void client.hostApi
      .selectSession({ sessionId, agentId })
      .catch(() => undefined);
  }, [
    client,
    handle,
    node.agentTargetId,
    node.lastActiveAgentSessionId,
    workspace.sessions
  ]);

  const agentDirectory = useMemo<AgentGUIProps["agentDirectory"]>(
    () => ({
      status: "ready",
      error: null,
      capturedAtUnixMs: Date.now(),
      agents: executionTargets(workspace).map((descriptor) => {
        const identity = resolveAgentGUIProviderIdentity(descriptor.provider);
        const iconUrl =
          descriptor.iconUrl ||
          resolveProviderIconAsset(identity?.iconKey, "providerRail") ||
          genericAgentIcon;
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
            status:
              descriptor.canInvoke && descriptor.runtimeBound
                ? "ready"
                : "unavailable",
            reason: descriptor.unavailableReason
          }
        };
      })
    }),
    [workspace]
  );
  const mentionAgentDirectory = useMemo<
    NonNullable<AgentGUIProps["mentionAgentDirectory"]>
  >(() => {
    const agents = new Map(
      agentDirectory.agents.map((entry) => [entry.agentTargetId, entry])
    );
    for (const {
      agent,
      provider,
      iconUrl,
      canInvoke,
      runtimeBound,
      unavailableReason
    } of workspace.agents) {
      agents.set(agent.id, {
        agentTargetId: agent.id,
        provider,
        name: agent.name,
        description: agent.description,
        iconUrl: iconUrl || genericAgentIcon,
        providerAccountUsageApplicable: false,
        availability: {
          status:
            canInvoke && runtimeBound && !agent.archived_at
              ? "ready"
              : "unavailable",
          reason: unavailableReason
        }
      });
    }
    return { ...agentDirectory, agents: [...agents.values()] };
  }, [agentDirectory, workspace.agents]);
  const activeSessionId = node.lastActiveAgentSessionId;
  // The empty composer's send button continues a stopped or interrupted local turn.
  const continuable = useSyncExternalStore(handle.subscribeTurns, () =>
    snapshot.local && activeSessionId
      ? handle.continuable(activeSessionId)
      : false
  );
  const composerEmptySubmit = useMemo(
    () =>
      continuable && activeSessionId
        ? {
            label: snapshot.locale === "zh-CN" ? "继续" : "Continue",
            onSubmit: () => handle.continueSession(activeSessionId)
          }
        : null,
    [continuable, activeSessionId, handle, snapshot.locale]
  );
  const imageSessionId = node.lastActiveAgentSessionId
    ? handle.toServerSessionId(node.lastActiveAgentSessionId)
    : null;
  const hostApi = useMemo(
    () => ({
      ...createAgentGuiHostApi(
          client,
          {
            workspaceId: snapshot.workspaceId,
            locale: snapshot.locale,
            local: snapshot.local
          },
          handle,
          imageSessionId
        ),
      ...(snapshot.local ? {
        composerCommands: composerCommands(
          snapshot.locale === "zh-CN",
          snapshot.sideWorkspace === true,
          (name, agentSessionId) => setComposerCommand({
            name, agentSessionId, sequence: ++commandSequence.current
          })
        )
      } : {})
    }),
    [
      client,
      handle,
      snapshot.locale,
      snapshot.workspaceId,
      snapshot.local,
      snapshot.sideWorkspace,
      imageSessionId
    ]
  );
  const mentionService = useMemo(
    () =>
      createMulticaContextMentions(
        client,
        {
          workspaceId: snapshot.workspaceId,
          locale: snapshot.locale,
          local: snapshot.local
        },
        handle
      ),
    [client, handle, snapshot.workspaceId, snapshot.locale, snapshot.local]
  );
  useEffect(() => () => mentionService.dispose(), [mentionService]);
  const prepareFiles = useMemo<
    NonNullable<AgentGUIProps["workspace"]["prepareExternalPromptFiles"]>
  >(
    () => async (files) =>
      Promise.all(
        files.map(async (file, sourceIndex) => {
          if (file.size > MAX_FILE_SIZE)
            return {
              sourceIndex,
              status: "error" as const,
              errorCode: "file_too_large" as const,
              retryable: false
            };
          try {
            const attachment = await handle.uploadAttachment(file);
            return {
              sourceIndex,
              status: "prepared" as const,
              file: {
                name: attachment.filename,
                url:
                  attachment.markdown_url ||
                  attachment.url ||
                  attachment.download_url,
                assetId: attachment.id,
                mimeType: attachment.content_type,
                sizeBytes: attachment.size_bytes,
                uploadStatus: "uploaded"
              }
            };
          } catch {
            return {
              sourceIndex,
              status: "error" as const,
              errorCode: "preparation_failed" as const,
              retryable: true
            };
          }
        })
      ),
    [handle]
  );

  const scopeProp1Value1 = initial.workspaceId;
  const scopeProp1Value2 = initial.workspaceSlug;
  const scopeProp1Value3 = snapshot.appOrigin;
  const scopeProp1 = useMemo<
    NonNullable<React.ComponentProps<typeof ResourceLinkProvider>["scope"]>
  >(
    () => ({
      workspaceId: scopeProp1Value1,
      workspaceSlug: scopeProp1Value2,
      appOrigin: scopeProp1Value3
    }),
    [scopeProp1Value1, scopeProp1Value2, scopeProp1Value3]
  );
  const identityProp2Value1 = snapshot.workspaceId;
  const identityProp2Value2 = snapshot.userId;
  const identityProp2 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentGUI>["identity"]>
  >(
    () => ({
      nodeId: instanceId,
      workspaceId: identityProp2Value1,
      currentUserId: identityProp2Value2,
      title: "Agent GUI"
    }),
    [instanceId, identityProp2Value1, identityProp2Value2]
  );

  const workspaceProp3 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentGUI>["workspace"]>
  >(
    () => ({
      path: "",
      agentSettings: { avoidGroupingEdits: false },
      prepareExternalPromptFiles: prepareFiles
    }),
    [prepareFiles]
  );
  const frameProp4Value1 = snapshot.hostActive ?? true;
  const frameProp4Value2 = snapshot.hostVisible ?? true;
  const frameProp4 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentGUI>["frame"]>
  >(
    () => ({
      position: { x: 0, y: 0 },
      ...size,
      desktopSize: size,
      isActive: frameProp4Value1,
      isVisible: frameProp4Value2,
      embedded: true
    }),
    [size, size, frameProp4Value1, frameProp4Value2]
  );
  const runtimeRequestsProp5Value1 = appends[0] ?? null;
  const runtimeRequestsProp5 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentGUI>["runtimeRequests"]>
  >(
    () => ({
      composerAppend: runtimeRequestsProp5Value1,
      composerFocusSequence: composerFocusSequence,
      workbench: { instanceId: instanceId }
    }),
    [runtimeRequestsProp5Value1, composerFocusSequence, instanceId]
  );

  const disabledProp6 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentGUI>["disabled"]>
  >(() => ["meet-tutti", "import-session"], []);

  const hostCapabilitiesProp7 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentGUI>["hostCapabilities"]>
  >(
    () => ({
      hiddenMentionFilterIds: HIDDEN_MENTION_FILTER_IDS,
      mentionService: mentionService,
      sessionWorktreeEnabled: false,
      capabilityControlsReadOnly: true,
      capabilityMenuState: {
        connectors: { enabled: false },
        computerUse: { presentationSupported: false }
      }
    }),
    [HIDDEN_MENTION_FILTER_IDS, mentionService]
  );
  const hostActionsProp8Value1 = usePersistCallback<
    NonNullable<
      NonNullable<
        React.ComponentProps<typeof AgentGUI>["hostActions"]
      >["onClose"]
    >
  >(() => undefined);
  const hostActionsProp8Value2 = usePersistCallback<
    NonNullable<
      NonNullable<
        React.ComponentProps<typeof AgentGUI>["hostActions"]
      >["onResize"]
    >
  >(() => undefined);
  const hostActionsProp8Value3 = usePersistCallback<
    NonNullable<
      NonNullable<
        React.ComponentProps<typeof AgentGUI>["hostActions"]
      >["onShowMessage"]
    >
  >((message, tone) => {
    void client.hostApi.showMessage({ message, tone });
  });
  const hostActionsProp8Value4 = usePersistCallback<
    NonNullable<
      NonNullable<
        React.ComponentProps<typeof AgentGUI>["hostActions"]
      >["onComposerAppendHandled"]
    >
  >((sequence) => {
    const quoteRequest = appends.find((request) => request.sequence === sequence)?.quote;
    const restoreRequestId = appendRestoreIds.current.get(sequence);
    appendRestoreIds.current.delete(sequence);
    appliedAppends.current.add(sequence);
    setAppends((current) =>
      current.filter((request) => request.sequence !== sequence)
    );
    if (quoteRequest) {
      setComposerFocusSequence((current) => (current ?? 0) + 1);
      return;
    }
    void client.hostApi.promptApplied({
      sequence,
      restoreRequestId
    });
  });
  const addSelectionQuote = usePersistCallback((quote: SelectedQuote) => {
    const guiSessionId = node.lastActiveAgentSessionId;
    if (!guiSessionId || handle.toServerSessionId(guiSessionId) !== quote.sessionId) return;
    const sequence = --localAppendSequence.current;
    setAppends((current) => [...current, {
      sequence,
      agentSessionId: guiSessionId,
      quote: { type: "quote", id: crypto.randomUUID(), text: quote.text, sourceName: quote.sourceName }
    }]);
  });
  const hostActionsProp8 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentGUI>["hostActions"]>
  >(
    () => ({
      onClose: hostActionsProp8Value1,
      onResize: hostActionsProp8Value2,
      onUpdateNode: setNode,
      onShowMessage: hostActionsProp8Value3,
      onComposerAppendHandled: hostActionsProp8Value4
    }),
    [
      hostActionsProp8Value1,
      hostActionsProp8Value2,
      setNode,
      hostActionsProp8Value3,
      hostActionsProp8Value4
    ]
  );
  const onComposerCommandHandled = usePersistCallback(() => setComposerCommand(null));
  const renderSlotsProp9Value1 = useMemo<
    NonNullable<
      NonNullable<
        React.ComponentProps<typeof AgentGUI>["renderSlots"]
      >["composerFooterAccessory"]
    >
  >(
    () => (context) => (
      <ComposerFooterControlsBinding1
        commandRequest={composerCommand}
        onCommandHandled={onComposerCommandHandled}
        context={context}
        client={client}
        handle={handle}
        workspace={workspace}
        snapshot={snapshot}
        setComposerFocusSequence={setComposerFocusSequence}
        pendingNewConversation={pendingNewConversation}
        setPendingNewConversation={setPendingNewConversation}
        instanceId={instanceId}
        pendingTarget={pendingTarget}
        setPendingTarget={setPendingTarget}
        setNode={setNode}
      />
    ),
    [
      composerCommand,
      onComposerCommandHandled,
      client,
      handle,
      workspace,
      snapshot,
      setComposerFocusSequence,
      pendingNewConversation,
      setPendingNewConversation,
      instanceId,
      pendingTarget,
      setPendingTarget,
      setNode
    ]
  );
  const renderSlotsProp9 = useMemo<
    NonNullable<React.ComponentProps<typeof AgentGUI>["renderSlots"]>
  >(
    () => ({
      composerEmptySubmit: composerEmptySubmit,
      composerFooterAccessory: renderSlotsProp9Value1
    }),
    [composerEmptySubmit, renderSlotsProp9Value1]
  );
  return (
    <ComposerDraftProvider
      initial={initial}
      legacyAliases={handle.legacySessionAliases()}
    >
      <InvokedAgentsProvider value={workspace.agents}>
        <ResourceLinkProvider
          client={client}
          scope={scopeProp1}
          agents={workspace.agents}
        >
          <SideEntryProvider
            root={root}
            enabled={snapshot.sideWorkspace === true}
            sidePane={snapshot.sidePane}
            client={client}
            handle={handle}
            sessionId={
              node.lastActiveAgentSessionId
                ? handle.toServerSessionId(node.lastActiveAgentSessionId)
                : null
            }
            chinese={snapshot.locale === "zh-CN"}
            onAddQuote={addSelectionQuote}
            quoteSourceName={agentDirectory.agents.find((agent) => agent.agentTargetId === node.agentTargetId)?.name}
          >
            <AgentGUI
              identity={identityProp2}
              workspace={workspaceProp3}
              frame={frameProp4}
              state={node}
              runtimeRequests={runtimeRequestsProp5}
              agentDirectory={agentDirectory}
              mentionAgentDirectory={mentionAgentDirectory}
              agentActivityRuntime={handle.runtime}
              agentHostApi={hostApi}
              locale={snapshot.locale}
              disabled={disabledProp6}
              hostCapabilities={hostCapabilitiesProp7}
              hostActions={hostActionsProp8}
              renderSlots={renderSlotsProp9}
            />
          </SideEntryProvider>
        </ResourceLinkProvider>
      </InvokedAgentsProvider>
    </ComposerDraftProvider>
  );
}

const ComposerFooterControlsBinding1 = observer(
  function ComposerFooterControlsBinding1({
    commandRequest,
    onCommandHandled,
    context,
    client,
    handle,
    workspace,
    snapshot,
    setComposerFocusSequence,
    pendingNewConversation,
    setPendingNewConversation,
    instanceId,
    pendingTarget,
    setPendingTarget,
    setNode
  }: ComposerFooterControlsBinding1Props) {
    const executionTargetOptions = useMemo(
      () => executionTargets(workspace),
      [workspace]
    );
    const onNewConversationAppliedProp10 = usePersistCallback<
      NonNullable<
        React.ComponentProps<
          typeof ComposerFooterControls
        >["onNewConversationApplied"]
      >
    >(() => setPendingNewConversation(undefined));
    const onOpenNewConversationProp11 = usePersistCallback<
      NonNullable<
        React.ComponentProps<
          typeof ComposerFooterControls
        >["onOpenNewConversation"]
      >
    >(() =>
      dispatchAgentGuiWorkbenchCommand({
        type: "new-conversation",
        instanceId
      })
    );
    const onCancelTargetSwitchProp12 = usePersistCallback<
      NonNullable<
        React.ComponentProps<
          typeof ComposerFooterControls
        >["onCancelTargetSwitch"]
      >
    >(() => setPendingTarget(null));
    const onLeaveSessionProp13 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ComposerFooterControls>["onLeaveSession"]
      >
    >((target, fromSession, draft, projectPath) => {
      setPendingTarget({
        target,
        fromSession,
        draft,
        projectPath
      });
      setNode((current) => ({
        ...current,
        lastActiveAgentSessionId: null
      }));
    });
    const onSelectAgentProp14 = usePersistCallback<
      NonNullable<
        React.ComponentProps<typeof ComposerFooterControls>["onSelectAgent"]
      >
    >((target) => {
      setPendingTarget(null);
      setNode((current) =>
        current.agentTargetId === target.id
          ? current
          : {
              ...current,
              provider: target.provider,
              agentTargetId: target.id,
              lastActiveAgentSessionId: null
            }
      );
    });
    return (
      <ComposerFooterControls
        commandRequest={commandRequest}
        onCommandHandled={onCommandHandled}
        context={context}
        client={client}
        handle={handle}
        targets={executionTargetOptions}
        snapshot={snapshot}
        onReadyForFocus={setComposerFocusSequence}
        newConversationRequest={pendingNewConversation}
        onNewConversationApplied={onNewConversationAppliedProp10}
        onOpenNewConversation={onOpenNewConversationProp11}
        pendingTarget={pendingTarget}
        onCancelTargetSwitch={onCancelTargetSwitchProp12}
        onLeaveSession={onLeaveSessionProp13}
        onSelectAgent={onSelectAgentProp14}
      />
    );
  }
);

type ComposerFooterControlsBinding1Props = {
  commandRequest: ComposerCommandRequest | null;
  onCommandHandled(): void;
  context: import("../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/AgentGUINodeView").AgentGUIComposerFooterAccessoryContext;
  client: NonNullable<{
    client: AgentGuiHostServices;
    initial: AgentGuiInitSnapshot;
    handle: RuntimeHandle;
    root: HTMLElement;
  }>["client"];
  handle: NonNullable<{
    client: AgentGuiHostServices;
    initial: AgentGuiInitSnapshot;
    handle: RuntimeHandle;
    root: HTMLElement;
  }>["handle"];
  workspace: import("./protocol").AgentGuiWorkspaceState;
  snapshot: AgentGuiInitSnapshot;
  setComposerFocusSequence: import("react").Dispatch<
    import("react").SetStateAction<number | null>
  >;
  pendingNewConversation:
    | import("./protocol").AgentGuiNewConversationRequest
    | undefined;
  setPendingNewConversation: import("react").Dispatch<
    import("react").SetStateAction<
      import("./protocol").AgentGuiNewConversationRequest | undefined
    >
  >;
  instanceId: string;
  pendingTarget: {
    target: ReturnType<typeof executionTargets>[number];
    fromSession: string;
    draft?: ProviderDraft;
    projectPath?: string | null;
  } | null;
  setPendingTarget: import("react").Dispatch<
    import("react").SetStateAction<{
      target: ReturnType<typeof executionTargets>[number];
      fromSession: string;
      draft?: ProviderDraft;
      projectPath?: string | null;
    } | null>
  >;
  setNode: import("react").Dispatch<
    import("react").SetStateAction<
      import("../../../vendor/tutti/packages/agent/gui/types").AgentGUINodeData
    >
  >;
};
