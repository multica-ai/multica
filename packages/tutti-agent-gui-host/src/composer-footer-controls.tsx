import { useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import { Button, DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubTrigger, DropdownMenuSubContent } from "@tutti-os/ui-system/components";
import { ChevronDownIcon } from "@tutti-os/ui-system/icons";
import type { AgentGUIProps } from "@tutti-os/agent-gui/agent-gui";
import type { RuntimeModelsResult } from "@multica/core/types";
import type { AgentGuiExecutionTarget, AgentGuiInitSnapshot, AgentGuiRpcClient } from "./protocol";
import type { createMulticaAgentGuiRuntime } from "./runtime";
import { localReasoningLevels } from "@multica/core/local-conversation/protocol";
import { UsageIndicator } from "./usage-indicator";
import { PermissionPicker } from "./permission-picker";
import type { AgentComposerProps } from "../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/AgentComposer";

export type ProviderDraft = AgentComposerProps["draftContent"];
type FooterContext = Parameters<NonNullable<NonNullable<AgentGUIProps["renderSlots"]>["composerFooterAccessory"]>>[0]
  & Partial<Pick<AgentComposerProps, "draftContent" | "onDraftContentChange" | "onProjectPathChange">>;
type Handle = ReturnType<typeof createMulticaAgentGuiRuntime>;
interface Props {
  context: FooterContext;
  client: AgentGuiRpcClient;
  handle: Handle;
  targets: AgentGuiExecutionTarget[];
  snapshot: AgentGuiInitSnapshot;
  newConversationRequest?: AgentGuiInitSnapshot["newConversationRequest"];
  onNewConversationApplied?(): void;
  onOpenNewConversation?(): void;
  pendingTarget?: { target: AgentGuiExecutionTarget; fromSession: string; draft?: ProviderDraft; projectPath?: string | null } | null;
  onLeaveSession(target: AgentGuiExecutionTarget, fromSession: string, draft?: ProviderDraft, projectPath?: string | null): void;
  onCancelTargetSwitch(): void;
  onSelectAgent(agent: AgentGuiExecutionTarget): void;
  onReadyForFocus(sequence: number): void;
}

export function ComposerFooterControls({ context, client, handle, targets, snapshot, newConversationRequest, onNewConversationApplied, onOpenNewConversation, pendingTarget = null, onLeaveSession, onCancelTargetSwitch, onSelectAgent, onReadyForFocus }: Props) {
  const modelValueId = useId();
  const agentValueId = useId();
  const onDraftContentChange = context.onDraftContentChange;
  const onProjectPathChange = context.onProjectPathChange;
  const chinese = snapshot.locale === "zh-CN";
  const uploadCount = useSyncExternalStore(handle.subscribeUploads, handle.activeUploadCount);
  const sessionId = context.agentSessionId || null;
  const serverSessionId = sessionId ? handle.toServerSessionId(sessionId) : null;
  // The rail selection is a default for new conversations, not the active
  // conversation's provider. Existing conversations own their execution target.
  const agentId = snapshot.sessions.find((entry) => entry.id === serverSessionId)?.agent_id
    ?? context.selectedAgentTarget?.agentTargetId;
  const agent = targets.find((entry) => entry.id === agentId);
  const deliveredFocus = useRef<number | null>(null);
  const appliedNewConversation = useRef<number | null>(null);
  useEffect(() => {
    if (!snapshot.local || snapshot.activeSessionId || !context.isActive || context.isSubmittingPrompt || !onProjectPathChange
      || !newConversationRequest || agentId !== snapshot.selectedAgentId
      || appliedNewConversation.current === newConversationRequest.sequence) return;
    if (sessionId) {
      // The GUI can attach a session before the host selection echo arrives.
      onOpenNewConversation?.();
      return;
    }
    appliedNewConversation.current = newConversationRequest.sequence;
    onProjectPathChange(newConversationRequest.projectPath);
    onReadyForFocus(newConversationRequest.sequence);
    onNewConversationApplied?.();
  }, [snapshot.local, snapshot.activeSessionId, snapshot.selectedAgentId, sessionId, context.isActive, context.isSubmittingPrompt, agentId, newConversationRequest, onProjectPathChange, onReadyForFocus, onNewConversationApplied, onOpenNewConversation]);
  useEffect(() => {
    if (!snapshot.composerFocusSequence || !context.isActive || !sessionId
      || deliveredFocus.current === snapshot.composerFocusSequence
      || handle.toServerSessionId(sessionId) !== snapshot.activeSessionId) return;
    let current = true;
    const sequence = snapshot.composerFocusSequence;
    // Reuse session loading before handing focus to its actual composer.
    void handle.runtime.getSession(snapshot.workspaceId, sessionId).then(() => {
      if (!current) return;
      deliveredFocus.current = sequence;
      onReadyForFocus(sequence);
    }).catch(() => undefined);
    return () => { current = false; };
  }, [snapshot.composerFocusSequence, snapshot.activeSessionId, snapshot.workspaceId, context.isActive, sessionId, handle, onReadyForFocus]);
  useSyncExternalStore(handle.subscribeModels, handle.getModelRevision);
  const selected = agentId ? handle.getModelSelection(agentId, sessionId) : null;
  const selectedEffort = agentId ? handle.getReasoningSelection(agentId, sessionId) : null;
  const [catalog, setCatalog] = useState<RuntimeModelsResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const request = useRef(0);
  const abort = useRef<AbortController | null>(null);
  const busy = Boolean(pendingTarget || context.isSendingTurn || context.isSubmittingPrompt || handle.isModelSelectionBusy(sessionId));
  useEffect(() => {
    if (!pendingTarget) return;
    // The imported GUI first reconciles its active conversation. Changing its
    // target earlier lets that conversation's metadata restore the old target.
    if (!sessionId) {
      if (pendingTarget.draft) onDraftContentChange?.(pendingTarget.draft, "home");
      if (snapshot.local) onProjectPathChange?.(pendingTarget.projectPath ?? null);
      onSelectAgent(pendingTarget.target);
    } else if (sessionId !== pendingTarget.fromSession) onCancelTargetSwitch();
  }, [pendingTarget, sessionId, onDraftContentChange, onProjectPathChange, snapshot.local, onSelectAgent, onCancelTargetSwitch]);
  const unavailable = !agent?.canInvoke || !agent.runtimeBound;
  const labels = chinese ? {
    model: "模型", agent: snapshot.local ? "本机运行器" : "智能体", default: "默认", loading: "正在加载模型…", retry: "重试", refresh: "刷新模型",
    failed: "模型目录加载失败", empty: "没有可用模型", unsupported: "此运行时不支持选择模型", unknown: "当前配置，未列入目录",
    running: "运行或提交期间无法切换", switch: snapshot.local ? "切换运行器会保留当前输入。" : "切换智能体会新建会话，原草稿保留在原会话。", runtime: snapshot.local ? "未在本机安装" : "未绑定运行时", denied: "无调用权限",
    defaultModel: "默认模型", modelScope: "用于当前会话的运行器；@ 智能体使用其自身配置。", online: snapshot.local ? "已安装" : "在线", offline: "离线 · 发送后等待连接",
  } : {
    model: "Model", agent: snapshot.local ? "Local provider" : "Agent", default: "Default", loading: "Loading models…", retry: "Retry", refresh: "Refresh models",
    failed: "Could not load model catalog", empty: "No models available", unsupported: "This runtime does not support model selection", unknown: "Configured; absent from catalog",
    running: "Wait for the run or submission to finish", switch: snapshot.local ? "Your current input is kept when switching providers." : "Switching agents starts a new conversation. The original draft stays in its conversation.", runtime: snapshot.local ? "Not installed locally" : "No runtime bound", denied: "Invocation not allowed",
    defaultModel: "Default model", modelScope: "Applies to this conversation's provider. Mentioned agents use their own configuration.", online: snapshot.local ? "Installed" : "Online", offline: "Offline · sends wait for connection",
  };

  useEffect(() => {
    request.current++;
    abort.current?.abort();
    setCatalog(null); setLoading(false); setError(null);
    return () => { request.current++; abort.current?.abort(); };
  }, [agentId, client]);

  const load = async (force = false) => {
    if (!agentId || unavailable || loading) return;
    const sequence = ++request.current;
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setLoading(true); setError(null);
    try {
      const result = await handle.loadModelCatalog(agentId, { force, signal: controller.signal });
      if (request.current === sequence) setCatalog(result);
    } catch (failure) {
      if (request.current === sequence && !controller.signal.aborted) setError(failure instanceof Error ? failure.message : labels.failed);
    } finally { if (request.current === sequence) setLoading(false); }
  };
  const changeModel = async (value: string) => {
    if (!agentId || busy || saving || unavailable) return;
    setSaving(true); setError(null);
    try { await handle.setModelSelection(agentId, sessionId, value === "" ? null : value); }
    catch (failure) {
      const message = failure instanceof Error ? failure.message : String(failure);
      setError(message);
      void client.request("host.showMessage", { message, tone: "error" }).catch(() => undefined);
    } finally { setSaving(false); }
  };
  const changeEffort = async (value: string) => {
    if (!agentId || busy || saving || unavailable) return;
    setSaving(true); setError(null);
    try { await handle.setReasoningSelection(agentId, sessionId, value || null); }
    catch (failure) {
      const message = failure instanceof Error ? failure.message : String(failure);
      setError(message);
      void client.request("host.showMessage", { message, tone: "error" }).catch(() => undefined);
    } finally { setSaving(false); }
  };
  const configured = selected ?? agent?.model ?? null;
  const known = catalog?.models.find((entry) => entry.id === configured);
  const defaultModel = catalog?.models.find((entry) => entry.id === agent?.model)?.label
    || agent?.model || catalog?.models.find((entry) => entry.default)?.label;
  const levels = localReasoningLevels(catalog?.models ?? [], configured);
  const effortLabel = (value: string, fallback: string) => chinese ? ({ none: "无", minimal: "极低", low: "低", medium: "中", high: "高", xhigh: "极高", max: "最高", ultra: "超高" }[value] ?? fallback) : fallback;
  const currentLabel = selected ? known?.label || selected : defaultModel ? `${labels.default} · ${defaultModel}` : labels.defaultModel;

  return <div className="multica-composer-footer-controls">
    {uploadCount > 0 && <span role="status" className="multica-composer-footer-help">{chinese ? `正在上传 ${uploadCount} 个附件` : `Uploading ${uploadCount} ${uploadCount === 1 ? "attachment" : "attachments"}`}</span>}
    {snapshot.local && agentId && <PermissionPicker handle={handle} client={client} agentId={agentId} sessionId={sessionId} chinese={chinese} disabled={busy || saving || unavailable} />}
    {snapshot.local && agentId && <UsageIndicator client={client} localConversations={handle.localConversations} agentId={agentId} sessionId={serverSessionId} chinese={chinese} busy={busy} disabled={unavailable || snapshot.sessions.find((entry) => entry.id === serverSessionId)?.status === "archived"} active={snapshot.hostVisible !== false && snapshot.hostActive !== false} />}
    <DropdownMenu onOpenChange={(open) => { if (open && !catalog && !loading) void load(); }}>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" data-testid="multica-model-picker" aria-label={labels.model}
          aria-describedby={modelValueId}
          disabled={busy || saving || unavailable} aria-busy={loading || saving || undefined} title={busy ? labels.running : currentLabel}>
          <span id={modelValueId} className="multica-composer-footer-control-label">{currentLabel}{selectedEffort ? ` · ${effortLabel(selectedEffort, levels.find((level) => level.value === selectedEffort)?.label ?? selectedEffort)}` : ""}</span>
          <ChevronDownIcon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      {/* Portals remain React descendants of the source window click-capture guard. */}
      <DropdownMenuContent className="multica-composer-footer-menu multica-model-picker-menu nodrag" align="end" side="top" collisionPadding={16}>
        <DropdownMenuLabel>{labels.model}</DropdownMenuLabel>
        <div className="multica-model-picker-list">
          <DropdownMenuRadioGroup value={selected ?? ""} onValueChange={(value) => { void changeModel(value); }}>
            <DropdownMenuRadioItem value="" disabled={busy || saving}>{labels.default}{defaultModel ? ` · ${defaultModel}` : ""}</DropdownMenuRadioItem>
            {catalog?.supported !== false && catalog?.models.map((model) => <DropdownMenuRadioItem key={model.id} value={model.id} disabled={busy || saving} title={model.id}>{model.label || model.id}</DropdownMenuRadioItem>)}
            {configured && catalog && !known && <DropdownMenuItem disabled>{configured} · {labels.unknown}</DropdownMenuItem>}
            {catalog?.unavailableModels?.map((model) => <DropdownMenuItem key={`unavailable:${model.id}`} disabled title={model.reason}>{model.label || model.id}{model.reason ? ` · ${model.reason}` : ""}</DropdownMenuItem>)}
          </DropdownMenuRadioGroup>
          {loading && <DropdownMenuItem disabled>{labels.loading}</DropdownMenuItem>}
          {catalog?.supported === false && <DropdownMenuItem disabled>{labels.unsupported}</DropdownMenuItem>}
          {catalog?.supported && catalog.models.length === 0 && <DropdownMenuItem disabled>{labels.empty}</DropdownMenuItem>}
          {error && <div className="multica-composer-footer-error" role="alert">{labels.failed}: {error}</div>}
        </div>
        {snapshot.local && catalog && <>
          <DropdownMenuSeparator />
          {levels.length > 0 ? <DropdownMenuSub>
            <DropdownMenuSubTrigger disabled={busy || saving} className="multica-model-picker-effort">
              <span>{chinese ? "思考强度" : "Thinking effort"}</span>
              <span className="multica-model-picker-effort-value">{selectedEffort ? effortLabel(selectedEffort, levels.find((level) => level.value === selectedEffort)?.label ?? selectedEffort) : labels.default}</span>
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="multica-composer-footer-menu multica-model-picker-effort-menu nodrag" collisionPadding={16}>
              <DropdownMenuLabel>{chinese ? "思考强度" : "Thinking effort"}</DropdownMenuLabel>
              <DropdownMenuRadioGroup value={selectedEffort ?? ""} onValueChange={(value) => { void changeEffort(value); }}>
                <DropdownMenuRadioItem value="" disabled={busy || saving}>{labels.default}</DropdownMenuRadioItem>
                {levels.map((level) => <DropdownMenuRadioItem key={level.value} value={level.value} disabled={busy || saving} title={level.description}>{effortLabel(level.value, level.label)}</DropdownMenuRadioItem>)}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub> : <DropdownMenuItem disabled>{configured
            ? (chinese ? "此模型未提供可用强度" : "No effort options reported for this model")
            : (chinese ? "选择模型后可调整思考强度" : "Choose a model to adjust thinking effort")}</DropdownMenuItem>}
        </>}
        {(!snapshot.local || error) && <>
          <DropdownMenuSeparator />
          <DropdownMenuItem disabled={loading || busy} onSelect={(event) => { event.preventDefault(); void load(true); }}>{error ? labels.retry : labels.refresh}</DropdownMenuItem>
        </>}
        {!snapshot.local && <p className="multica-composer-footer-help">{labels.modelScope}</p>}
      </DropdownMenuContent>
    </DropdownMenu>
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" data-testid="multica-agent-picker" aria-label={labels.agent} aria-describedby={agentValueId} disabled={busy || saving || snapshot.sidePane === true} title={snapshot.sidePane ? (chinese ? "旁支沿用来源会话的运行器，可单独选择模型。" : "Side conversations use their source provider; their model can be changed independently.") : busy ? labels.running : labels.switch}>
          <span id={agentValueId} className="multica-composer-footer-control-label">{agent?.name || labels.agent}</span>
          <ChevronDownIcon aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent className="multica-composer-footer-menu nodrag" align="end" side="top">
        <DropdownMenuLabel>{labels.agent}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={agentId ?? ""} onValueChange={(id) => {
          if (id === agentId || busy || saving) return;
          const target = targets.find((entry) => entry.id === id);
          if (target?.canInvoke && target.runtimeBound) {
            if (sessionId) onLeaveSession(target, sessionId, snapshot.local ? context.draftContent : undefined, snapshot.local ? context.composerSettings?.selectedProjectPath : undefined);
            else onSelectAgent(target);
          }
        }}>
          {targets.map((target) => {
            const reason = !target.runtimeBound ? labels.runtime : !target.canInvoke ? target.unavailableReason || labels.denied : "";
            return <DropdownMenuRadioItem key={target.id} value={target.id} disabled={busy || saving || Boolean(reason)} title={reason || undefined} aria-label={target.name}>
              <span className="multica-composer-agent-option"><span>{target.name}</span>
                <span className="multica-composer-agent-availability">{reason || (target.runtimeOnline ? labels.online : labels.offline)}</span>
              </span>
            </DropdownMenuRadioItem>;
          })}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <p className="multica-composer-footer-help">{labels.switch}</p>
      </DropdownMenuContent>
    </DropdownMenu>
  </div>;
}
